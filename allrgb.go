package main

// TODO compress backup file
// Check on Closing stmt after use (in code but does it work?)

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mitchellh/go-homedir"
	"github.com/nfnt/resize"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Alignment for when src/dest don't match dimensions
type Alignment int8

// Aspect - describes dest image
type Aspect struct {
	Width  int
	Height int
	Ratio  float64
}

// ColorRow from db
type ColorRow struct {
	Found bool
	Dist  uint8
	R     uint8
	G     uint8
	B     uint8
}

const (
	// Start alignment
	Start Alignment = -1
	// Center alignment
	Center Alignment = 0
	// End alignment
	End Alignment = 1
)
const totalColors int64 = 16777216
const vacuumFreq = 131072
const dbFileName = "allrgb.db"
const backupDbFileName = "allrgb.db.backup"
const storageFolderName = ".allrgb"

var storagePath string
var destFileName string
var dbPath string
var backupPath string
var p *message.Printer
var db *sql.DB
var aspectRatios = []Aspect{
	Aspect{Width: 1024, Height: 16384, Ratio: 0.0625},
	Aspect{Width: 2048, Height: 8192, Ratio: 0.25},
	Aspect{Width: 4096, Height: 4096, Ratio: 1},
	Aspect{Width: 8192, Height: 2048, Ratio: 4},
	Aspect{Width: 16384, Height: 1024, Ratio: 16},
}
var xOffsets = [][]int{
	[]int{0},
	[]int{0, 1, 1, 0},
	[]int{0, 2, 2, 0, 1, 1, 2, 1, 0},
	[]int{0, 2, 2, 0, 1, 3, 3, 1, 1, 3, 3, 1, 0, 2, 2, 0},
}
var yOffsets = [][]int{
	[]int{0},
	[]int{0, 1, 0, 1},
	[]int{0, 2, 0, 2, 1, 2, 1, 0, 1},
	[]int{0, 2, 0, 2, 1, 3, 1, 3, 0, 2, 0, 2, 1, 3, 1, 3},
}

// region utils
func check(e error, msg string) {
	if e != nil {
		fmt.Println("Error:", e)
		panic(msg)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func touch(path string) *os.File {
	fmt.Println("Creating:", path)
	file, err := os.Create(path)
	check(err, "Error creating "+path)
	return file
}

func open(path string) *os.File {
	fmt.Println("Opening:", path)
	file, err := os.Open(path)
	check(err, "Error Opening "+path)
	return file
}

func delete(path string) {
	fmt.Println("Deleting :" + path)
	err := os.Remove(path)
	check(err, "Error deleting "+path)
}

// endregion utils

// region draw

func calculateNewHeight(dWidth int, sWidth int, sHeight int) (newWidth int, newHeight int) {
	nWidth := dWidth
	nHeight := int(math.Ceil(float64(dWidth) * float64(sHeight) / float64(sWidth)))
	return nWidth, nHeight
}

func calculateNewWidth(dHeight int, sWidth int, sHeight int) (newWidth int, newHeight int) {
	nHeight := dHeight
	nWidth := int(math.Ceil(float64(sWidth) * float64(dHeight) / float64(sHeight)))
	return nWidth, nHeight
}

func drawImage(db *sql.DB, srcFile *os.File, destFile *os.File, gridSize int, align Alignment) {
	var srcResizedImage image.Image
	var newHeight int
	var newWidth int
	fmt.Println("Running <draw>…", time.Now().Unix())
	srcImage := decodeImage(srcFile)
	srcWidth := srcImage.Bounds().Max.X
	srcHeight := srcImage.Bounds().Max.Y
	aspectRatio := findAspectRatio(srcWidth, srcHeight)
	destWidth := aspectRatio.Width
	destHeight := aspectRatio.Height
	destImage := image.NewRGBA(image.Rect(0, 0, destWidth, destHeight))
	sImage := image.NewRGBA(image.Rect(0, 0, destWidth, destHeight))
	fmt.Println("Ready to resize", srcWidth, srcHeight, "->", aspectRatio)
	newWidth, newHeight = calculateNewHeight(destWidth, srcWidth, srcHeight)
	if newHeight < destHeight {
		newWidth, newHeight = calculateNewWidth(destHeight, srcWidth, srcHeight)
	}
	srcResizedImage = resize.Resize(uint(newWidth), uint(newHeight), srcImage, resize.Lanczos3)
	fmt.Println("Resized", srcResizedImage.Bounds())
	srcWidth = srcResizedImage.Bounds().Max.X
	srcHeight = srcResizedImage.Bounds().Max.Y
	startPoint := getStartingPoint(srcWidth, srcHeight, destWidth, destHeight, align)
	sRect := image.Rect(startPoint.X, startPoint.Y, startPoint.X+newWidth, startPoint.Y+newHeight)
	draw.Draw(sImage, sRect, srcResizedImage, image.ZP, draw.Src)

	// debugFileName, _ := homedir.Expand("~/Desktop/Debug.png")
	// fmt.Println("-- Saving debug File", debugFileName)
	// debugFile := touch(debugFileName)
	// debugFile.Seek(0, 0)
	// png.Encode(debugFile, sImage)
	// debugFile.Close()

	fmt.Println("ready to convert")
	convertImage(db, sImage, destImage, int(gridSize))
	fmt.Println("encoding")
	png.Encode(destFile, destImage)
	destFile.Close()
	err := os.Rename(destFileName+".part", destFileName)
	check(err, "Error saving to file")
}

func convertImage(db *sql.DB, srcImage *image.RGBA, destImage *image.RGBA, gridSize int) {
	var vacStart int64
	passComplete := "************** Pass %d of %d completed (time elapsed: %d) **************\n"
	rowComplete := "row %d of %d completed for pass %d\n"
	vacuumingMsg := "Vacuuming completed in %d seconds\n"
	start := time.Now().Unix()
	width := destImage.Bounds().Max.X
	height := destImage.Bounds().Max.Y
	passCount := int(gridSize * gridSize)
	pass := 0
	xOffset := 0
	yOffset := 0
	x := 0
	y := 0
	i := 1
	px := 0
	for pass < passCount {
		xOffset = xOffsets[gridSize-1][pass]
		yOffset = yOffsets[gridSize-1][pass]
		rows := int(math.Floor(float64(height-yOffset) / float64(gridSize)))
		fmt.Println("xoff:", xOffset, "yoff:", yOffset, "gridSize:", gridSize, "pass:", pass)
		for y = yOffset; y < height; y += gridSize {
			for x = xOffset; x < width; x += gridSize {
				setColor(db, srcImage, destImage, x, y)
				if px == vacuumFreq {
					vacStart = time.Now().Unix()
					fmt.Println("#### VACUUMING DB ####")
					vacStmt, _ := db.Prepare("VACUUM")
					vacStmt.Exec()
					vacStmt.Close()
					px = 0
					fmt.Printf(vacuumingMsg, time.Now().Unix()-vacStart)
				}
				px++
			}
			if i%10 == 0 && i > 0 {
				fmt.Printf(rowComplete, i, rows, pass+1)
			}
			i++
		}
		fmt.Printf(passComplete, pass+1, passCount, time.Now().Unix()-start)
		pass++
	}
	fmt.Println("Finished converting image, duration:", time.Now().Unix()-start)
}

func setColor(db *sql.DB, srcImage *image.RGBA, destImage *image.RGBA, x int, y int) {
	srcColor := srcImage.RGBAAt(x, y)
	destColor := matchColor(db, srcColor, x%2)
	destImage.SetRGBA(x, y, destColor)
}

func matchColor(db *sql.DB, srcColor color.RGBA, tick int) color.RGBA {
	var matched ColorRow
	c1 := "<"
	c2 := ">"
	comp1 := c1
	comp2 := c2
	if tick == 1 {
		comp1 = c2
		comp2 = c1
	}
	lum := getLum(srcColor)
	row := fetchRow(db, lum, "=")
	if row.Found {
		matched = row
	} else {
		row1 := fetchRow(db, lum, comp1)
		if row1.Found {
			matched = row1
		} else {
			matched = fetchRow(db, lum, comp2)
		}
	}
	statement, _ := db.Prepare("DELETE FROM colors WHERE r = ? AND g = ? AND b = ?")
	statement.Exec(matched.R, matched.G, matched.B)
	defer statement.Close()
	newColor := color.RGBA{matched.R, matched.G, matched.B, 255}
	return newColor
}

func fetchRow(db *sql.DB, lum int, comparison string) ColorRow {
	var colorRow ColorRow
	var row *sql.Row
	if comparison == "=" {
		row = db.QueryRow(fmt.Sprintf(`SELECT '0' AS distance, r, g, b
FROM colors WHERE lum %s %d ORDER BY distance ASC LIMIT 1`, comparison, lum))
	} else {
		row = db.QueryRow(fmt.Sprintf(`SELECT ABS(lum - %d) AS distance, r, g, b
FROM colors WHERE lum %s %d ORDER BY distance ASC LIMIT 1`, lum, comparison, lum))
	}
	switch err := row.Scan(&colorRow.Dist, &colorRow.R, &colorRow.G, &colorRow.B); err {
	case sql.ErrNoRows:
		return ColorRow{Found: false, Dist: 0, R: 0, G: 0, B: 0}
	case nil:
		colorRow.Found = true
		return colorRow
	default:
		panic(err)
	}
}

func getLum(c color.RGBA) int {
	r, g, b, _ := c.RGBA()
	red := float64(r >> 8)
	green := float64(g >> 8)
	blue := float64(b >> 8)
	lum := int(math.Round((red * 0.3) + (green * 0.59) + (blue * 0.11)))
	return lum
}

func decodeImage(srcFile *os.File) image.Image {
	var srcImage image.Image
	_, imageType, err := image.Decode(srcFile)
	check(err, "Error reading image")
	srcFile.Seek(0, 0)
	fmt.Println("Image Type", imageType)
	if imageType == "png" {
		fmt.Println("Decoding PNG")
		srcImage, err = png.Decode(srcFile)
	}
	if imageType == "jpeg" {
		fmt.Println("Decoding JPEG")
		srcImage, err = jpeg.Decode(srcFile)
	}
	if imageType == "gif" {
		fmt.Println("Decoding GIF")
		srcImage, err = gif.Decode(srcFile)
	}
	check(err, "Error decoding image")
	return srcImage
}

func getStartingPoint(srcWidth, srcHeight, destWidth, destHeight int, align Alignment) image.Point {
	// fmt.Println("srcWidth:", srcWidth, "srcHeight:", srcHeight)
	// fmt.Println("destWidth:", destWidth, "destHeight:", destHeight)
	startPoint := image.Point{0, 0}
	if srcHeight > destHeight {
		heightDiff := srcHeight - destHeight
		if align == Center {
			startPoint = image.Point{0, -1 * int(math.Round(float64(heightDiff)/2))}
		} else if align == End {
			startPoint = image.Point{0, heightDiff}
		}
	} else if srcWidth > destWidth {
		widthDiff := srcWidth - destWidth
		if align == Center {
			startPoint = image.Point{-1 * int(math.Round(float64(widthDiff)/2)), 0}
		} else if align == End {
			startPoint = image.Point{0, widthDiff}
		}
	}
	fmt.Println("StartPoint", startPoint)
	return startPoint
}

func findAspectRatio(width int, height int) Aspect {
	var aspectRatio Aspect
	imageAR := float64(width) / float64(height)
	fmt.Println("src aspect ratio", imageAR)
	distance := float64(totalColors)
	for _, ar := range aspectRatios {
		arDist := math.Abs(imageAR - ar.Ratio)
		if arDist < distance {
			distance = arDist
			aspectRatio = ar
		}
	}
	return aspectRatio
}

// endregion draw

// region db
func prepDBFilesystem(cleanDB bool) {
	homeFolder, err := homedir.Dir()
	check(err, "Cannot detect homedir")
	os.Chdir(homeFolder)
	storagePath = path.Join(homeFolder, storageFolderName)
	dbPath = path.Join(storagePath, dbFileName)
	backupPath = path.Join(storagePath, backupDbFileName)
	// check folder exists
	if !exists(storagePath) {
		fmt.Println("Creating storage dir:", storagePath)
		mkdirErr := os.Mkdir(storageFolderName, 0755)
		check(mkdirErr, "Cannot create storage dir")
	}
	// check db file exists
	if cleanDB {
		removeDB()
		if exists(backupPath) {
			delete(backupPath)
		}
	}
	if !exists(dbPath) {
		touch(dbPath)
	}
}

func createTable() {
	fmt.Println("Creating fresh colors table…")
	statement1, _ := db.Prepare("DROP TABLE IF EXISTS colors")
	statement1.Exec()
	statement1.Close()
	statement2, _ := db.Prepare(`CREATE TABLE colors(
	r UNSIGNED TINYINT NOT NULL,
	g UNSIGNED TINYINT NOT NULL,
	b UNSIGNED TINYINT NOT NULL,
	lum UNSIGED TINYINT GENERATED ALWAYS AS (ROUND((r * 0.3) + (g * 0.59) + (b * 0.11), 0)) STORED,
	PRIMARY KEY(r, g, b)
)`)
	statement2.Exec()
	statement2.Close()
	statement3, _ := db.Prepare("CREATE INDEX color_lum ON colors(lum)")
	statement3.Exec()
	statement3.Close()
}

// TODO zip backup before save
func createBackup() {
	fmt.Println("Creating DB backup", backupPath, "…")
	in := open(dbPath)
	defer in.Close()
	out := touch(backupPath)
	defer out.Close()
	_, err := io.Copy(out, in)
	check(err, "Error copying DB File to Backup")
}

// TODO unzip backup before copy
func createDbFromBackup() {
	fmt.Println("Creating DB from backup…")
	in := open(backupPath)
	defer in.Close()
	out := touch(dbPath)
	defer out.Close()
	_, err := io.Copy(out, in)
	check(err, "Error copying Backup File to DB")
}

func fillTable() {
	fmt.Println("Filling colors table…")
	deleteStmt, _ := db.Prepare("DELETE FROM colors")
	deleteStmt.Exec()
	deleteStmt.Close()
	var red uint16
	var blue uint16
	var green uint16
	var vals []string
	var valArgs []interface{}
	total := 0
	dot := ""
	formattedTotal := ""
	now := time.Now()
	start := now.Unix()
	fmt.Println("Generating 16,777,216 unique colors…")
	for red = 0; red < 256; red++ {
		for green = 0; green < 256; green++ {
			vals = make([]string, 0, 256)
			valArgs = make([]interface{}, 0, 256*3)
			for blue = 0; blue < 256; blue++ {
				vals = append(vals, "(?, ?, ?)")
				valArgs = append(valArgs, red)
				valArgs = append(valArgs, green)
				valArgs = append(valArgs, blue)
				total++
				if total > 1 && total%1000000 == 0 {
					dot = dot + "."
					formattedTotal = fmt.Sprintf("%10v", p.Sprintf("%d", total))
					fmt.Printf("%s colors made %s\n", formattedTotal, dot)
				}
			}
			insertStmt := fmt.Sprintf("INSERT INTO colors (r, g, b) VALUES %s", strings.Join(vals, ","))
			_, err := db.Exec(insertStmt, valArgs...)
			check(err, "Error inserting into db")
		}
	}
	fmt.Printf("%08d colors made in %d seconds\n", total, time.Now().Unix()-start)
}

func doesTableExist() bool {
	var name string
	row := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='colors'")
	switch err := row.Scan(&name); err {
	case sql.ErrNoRows:
		return false
	case nil:
		return true
	default:
		panic(err)
	}
}

func isTableFull() bool {
	var count int64
	row := db.QueryRow("SELECT COUNT(*) AS count FROM colors")
	switch err := row.Scan(&count); err {
	case sql.ErrNoRows:
		return false
	case nil:
		return count == totalColors
	default:
		panic(err)
	}
}

func buildDB(cleanDB bool) {
	fmt.Println("Running <db>…")
	prepDBFilesystem(cleanDB)
	db, _ = sql.Open("sqlite3", dbPath)
	backupExists := exists(backupPath)
	if !doesTableExist() {
		if backupExists {
			createDbFromBackup()
		} else {
			createTable()
		}
	}
	if !isTableFull() {
		if backupExists {
			createDbFromBackup()
		} else {
			fillTable()
			createBackup()
		}
	}
	fmt.Println("DB ready…")
}

func removeDB() {
	if exists(dbPath) {
		delete(dbPath)
	}
}

// endregion db

// region main
func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("│ Error:", r)
			fmt.Println("╘═══════════════════════════════════════════════════════════════════════")
			fmt.Println("Run `./allrgb help` for help menu")
			fmt.Println("")
			os.Exit(0)
		}
	}()
	p = message.NewPrinter(language.English)
	args := os.Args[1:]
	command := "help"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "db":
		dbArgs := args[1:]
		cleanDB := false
		if len(dbArgs) == 1 && (dbArgs[0] == "clean" || dbArgs[0] == "force") {
			cleanDB = true
		}
		buildDB(cleanDB)
		defer db.Close()
		os.Exit(0)
	case "draw":
		drawArgs := args[1:]
		if len(drawArgs) == 1 {
			panic("Missing output file")
		}
		if len(drawArgs) == 0 {
			panic("Missing source file")
		}
		srcFile := open(drawArgs[0])
		destFileName = drawArgs[1]
		destFile := touch(drawArgs[1] + ".part")
		gridSize := 0
		align := Center
		if len(drawArgs) > 2 {
			gridSizeVal, gridSizeErr := strconv.ParseUint(drawArgs[2], 10, 8)
			if gridSizeErr != nil || gridSizeVal > 3 {
				panic("Invalid gridSize value")
			}
			gridSize = int(gridSizeVal) + 1
			if len(drawArgs) > 3 {
				alignVal, alignErr := strconv.ParseInt(drawArgs[3], 10, 8)
				if alignErr != nil || alignVal > 1 || alignVal < -1 {
					panic("Invalid align value")
				}
				switch int8(alignVal) {
				case -1:
					align = Start
				case 0:
					align = Center
				case 1:
					align = End
				}
			}
		}
		buildDB(false)
		drawImage(db, srcFile, destFile, gridSize, align)
		removeDB()
		defer db.Close()
	default:
		printHelpMenu()
	}
	os.Exit(0)
}

// endregion main

//region help
func printHelpMenu() {
	fmt.Println(`ALLRGB Help
════════════════════════════════════════════════════════════════════
./allrgb <command> <args>

Commands
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
help: print help menu
db:   prepare database
draw: draw image

    <db> arguments
    ┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈
    path: path to save database (default: ~/.allrgb)

    <draw> arguments
    ┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈
    sourceFile: source file to draw image from
    outputFile: file name of output
	  gridSize:  px to skip, allows better color allocation across entire image (default 0)
	    valid values are 0, 1, 2, 3 & generate corresponding px grids of 1, 4, 9, 16
    align:  optional alignment if dimensions don't match after resize (default 0)
	   -1: align with start (left or top)
		  0: align in center
		  1: alight with end (right or bottom)

Examples
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
./allrgb help
./allrgb db ~/.mydb
./allrgb draw input/file.png output/file.png 3 0`)
}

//endregion help

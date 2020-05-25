package main

// TODO make sure delete db file when finished making image
// TODO check srcImage and get aspect ratio
// TODO make new image with right # of px in similar aspect ratio

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
	ID   int
	Dist uint8
	R    uint8
	G    uint8
	B    uint8
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
var offsets = [][]int{
	[]int{0},
	[]int{0, 2, 3, 1},
	[]int{0, 7, 2, 8, 4, 6, 3, 5, 1},
	[]int{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5},
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
func drawImage(srcFile *os.File, destFile *os.File, blockSize int, align Alignment) {
	fmt.Println("Running <draw>…")
	srcImage := decodeImage(srcFile)
	fmt.Println("Decoded", srcImage.Bounds())
	srcWidth := srcImage.Bounds().Max.X
	srcHeight := srcImage.Bounds().Max.Y
	aspectRatio := findAspectRatio(srcWidth, srcHeight)
	destWidth := aspectRatio.Width
	destHeight := aspectRatio.Height
	fmt.Println("Creating images", aspectRatio)
	destImage := image.NewRGBA(image.Rect(0, 0, destWidth, destHeight))
	sImage := image.NewRGBA(image.Rect(0, 0, destWidth, destHeight))
	fmt.Println("Ready to resize", srcWidth, srcHeight, "->", aspectRatio)
	newHeight := math.Ceil(float64(destWidth) * float64(srcHeight) / float64(srcWidth))
	srcResizedImage := resize.Resize(uint(destWidth), uint(newHeight), srcImage, resize.Lanczos3)
	fmt.Println("Resized", srcResizedImage.Bounds())
	srcWidth = srcResizedImage.Bounds().Max.X
	srcHeight = srcResizedImage.Bounds().Max.Y
	startPoint := getStartingPoint(srcWidth, srcHeight, destWidth, destHeight, align)
	draw.Draw(sImage, destImage.Bounds(), srcResizedImage, startPoint, draw.Src)

	// debugFileName, _ := homedir.Expand("~/Desktop/Debug.png")
	// fmt.Println("-- Saving debug File", debugFileName)
	// debugFile := touch(debugFileName)
	// debugFile.Seek(0, 0)
	// png.Encode(debugFile, srcResizedImage)
	// debugFile.Close()

	fmt.Println("ready to convert")
	convertImage(db, srcImage, destImage, int(blockSize))
	fmt.Println("encoding")
	png.Encode(destFile, destImage)
	destFile.Close()
	err := os.Rename(destFileName+".part", destFileName)
	check(err, "Error saving to file")
}

func convertImage(db *sql.DB, srcImage image.Image, destImage *image.RGBA, blockSize int) {
	width := destImage.Bounds().Max.X
	height := destImage.Bounds().Max.Y
	passCount := int(blockSize * blockSize)
	pass := 0
	xOffset := 0
	yOffset := 0
	x := 0
	y := 0
	for pass < passCount {
		xOffset = offsets[blockSize-1][pass%blockSize]
		yOffset = offsets[blockSize-1][int(math.Floor(float64(pass)/float64(blockSize)))]
		y = yOffset
		x = xOffset
		for y < height {
			for x < width {
				setColor(db, srcImage, destImage, x, y)
				x = x + blockSize
			}
			y = y + blockSize
		}
		pass++
	}
}

func setColor(db *sql.DB, srcImage image.Image, destImage *image.RGBA, x int, y int) {
	srcColor := srcImage.At(x, y)
	destColor := matchColor(db, srcColor)
	destImage.Set(x, y, destColor)
}

func matchColor(db *sql.DB, srcColor color.Color) color.Color {
	var matched ColorRow
	lum := getLum(srcColor)
	row1 := fetchRow(db, lum, "<=")
	row2 := fetchRow(db, lum, ">")
	fmt.Println("Color", srcColor, "lum:", lum, "\n", row1, "\n", row2)
	if row1.ID == -1 {
		matched = row2
	} else if row2.ID == -1 {
		matched = row1
	} else if row1.Dist <= row2.Dist {
		matched = row1
	} else {
		matched = row2
	}
	statement, _ := db.Prepare("DELETE FROM colors WHERE r = ? AND g = ? AND b = ?")
	statement.Exec(matched.R, matched.G, matched.B)
	defer statement.Close()
	newColor := color.RGBA{matched.R, matched.G, matched.B, 255}
	fmt.Println("New Color", newColor)
	fmt.Println("Deleted ID", matched.R, matched.G, matched.B)
	return newColor
}

func fetchRow(db *sql.DB, lum int, direction string) ColorRow {
	var colorRow ColorRow
	query := fmt.Sprintf(`SELECT ABS(lum - %d) AS distance, r, g, b
FROM colors
WHERE %s
ORDER BY distance ASC
LIMIT 1`, lum, fmt.Sprintf("lum %s %d", direction, lum))
	row := db.QueryRow(query)
	fmt.Println("Row", row, "|", query)
	colorRow.ID = -1
	switch err := row.Scan(&colorRow.Dist, &colorRow.R, &colorRow.G, &colorRow.B); err {
	case sql.ErrNoRows:
		return ColorRow{ID: -1, Dist: 0, R: 0, G: 0, B: 0}
	case nil:
		return colorRow
	default:
		fmt.Println("--ERROR", err)
		panic(err)
	}
}

func getLum(c color.Color) int {
	r, g, b, _ := c.RGBA()
	red := float64(r >> 8)
	green := float64(g >> 8)
	blue := float64(b >> 8)
	lum := int(math.Round((red * 0.3) + (green * 0.59) + (blue * 0.11)))
	fmt.Println("Calculate lum", "r: ", red, "g: ", green, "b: ", blue, "lum: ", lum)
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
	fmt.Println("srcWidth:", srcWidth, "srcHeight:", srcHeight)
	fmt.Println("destWidth:", destWidth, "destHeight:", destHeight)
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
		if exists(dbPath) {
			delete(dbPath)
		}
		if exists(backupPath) {
			delete(backupPath)
		}
	}
	if !exists(dbPath) {
		_ = touch(dbPath)
	}
}

func createTable() {
	fmt.Println("Creating fresh colors table…")
	statement1, _ := db.Prepare("DROP TABLE IF EXISTS colors")
	statement1.Exec()
	statement2, _ := db.Prepare(`CREATE TABLE colors(
	id UNSIGNED INTEGER PRIMARY KEY,
	r UNSIGNED TINYINT NOT NULL,
	g UNSIGNED TINYINT NOT NULL,
	b UNSIGNED TINYINT NOT NULL,
	lum UNSIGED TINYINT GENERATED ALWAYS AS (ROUND((r * 0.3) + (g * 0.59) + (b * 0.11), 0)) STORED
)`)
	statement2.Exec()
	statement3, _ := db.Prepare("CREATE INDEX color_lum ON colors(lum)")
	statement3.Exec()
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
	for red < 256 {
		green = 0
		for green < 256 {
			blue = 0
			vals = make([]string, 0, 256)
			valArgs = make([]interface{}, 0, 256*3)
			for blue < 256 {
				vals = append(vals, "(?, ?, ?)")
				valArgs = append(valArgs, red)
				valArgs = append(valArgs, green)
				valArgs = append(valArgs, blue)
				blue++
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
			green++
		}
		red++
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
		fillTable()
		if !backupExists {
			createBackup()
		}
	}
	fmt.Println("DB ready…")
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
		blockSize := 0
		align := Center
		if len(drawArgs) > 2 {
			blockSizeVal, blockSizeErr := strconv.ParseUint(drawArgs[2], 10, 8)
			if blockSizeErr != nil || blockSizeVal > 3 {
				panic("Invalid blockSize value")
			}
			blockSize = int(blockSizeVal) + 1
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
		drawImage(srcFile, destFile, blockSize, align)
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
	stepSize:  px to skip, allows better color allocation across entire image (default 0)
	    valid values are (0, 1, 2, 3)
    align:  optional alignment if dimensions dont match after resize (default 0)
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

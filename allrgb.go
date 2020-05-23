package main

// TODO make sure delete db file when finished making image
// TODO check srcImage and get aspect ratio
// TODO make new image with right # of px in similar aspect ratio

import (
	"fmt"
	"database/sql"
	// "image"
	// "image/color"
	// "image/draw"
	// "image/png"
	// "archive/zip"
	"io"
	"os"
	"path"
	"strings"
	"strconv"
	"time"
   	"golang.org/x/text/language"
	"golang.org/x/text/message"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mitchellh/go-homedir"
)

type Alignment int8

type Aspect struct {
	Width int32
	Height int32
	Ratio float64
}

const (
	Start Alignment = -1
    Center Alignment = 0
    End Alignment = 1
)

var dbFileName = "allrgb.db"
var backupDbFileName = "allrgb.db.backup"
var srcFile *os.File
var destFile  *os.File
var dithering uint8 = 0
var storagePath string
var dbPath string
var backupPath string
var db *sql.DB
var p *message.Printer
var totalColors int64 = 16777216
var cleanDB bool = false
var align Alignment
var aspectRatio Aspect

var aspectRatios []Aspect = []Aspect {
	Aspect{ Width: 1, Height: 16777216, Ratio: 0.000000059604644775390625 }
	Aspect{ Width: 2, Height: 8388608, Ratio: 0.0000002384185791015625 }
	Aspect{ Width: 4, Height: 4194304, Ratio: 0.00000095367431640625 }
	Aspect{ Width: 8, Height: 2097152, Ratio: 0.000003814697265625 }
	Aspect{ Width: 16, Height: 1048576, Ratio: 0.0000152587890625 }
	Aspect{ Width: 32, Height: 524288, Ratio: 0.00006103515625 }
	Aspect{ Width: 64, Height: 262144, Ratio: 0.000244140625 }
	Aspect{ Width: 128, Height: 131072, Ratio: 0.0009765625 }
	Aspect{ Width: 256, Height: 65536, Ratio: 0.00390625 }
	Aspect{ Width: 512, Height: 32768, Ratio: 0.015625 }
	Aspect{ Width: 1024, Height: 16384, Ratio: 0.0625 }
	Aspect{ Width: 2048, Height: 8192, Ratio: 0.25 }
	Aspect{ Width: 4096, Height: 4096, Ratio: 1 }
	Aspect{ Width: 8192, Height: 2048, Ratio: 4 }
	Aspect{ Width: 16384, Height: 1024, Ratio: 16 }
	Aspect{ Width: 32768, Height: 512, Ratio: 64 }
	Aspect{ Width: 65536, Height: 256, Ratio: 256 }
	Aspect{ Width: 131072, Height: 128, Ratio: 1024 }
	Aspect{ Width: 262144, Height: 64, Ratio: 4096 }
	Aspect{ Width: 524288, Height: 32, Ratio: 16384 }
	Aspect{ Width: 1048576, Height: 16, Ratio: 65536 }
	Aspect{ Width: 2097152, Height: 8, Ratio: 262144 }
	Aspect{ Width: 4194304, Height: 4, Ratio: 1048576 }
	Aspect{ Width: 8388608, Height: 2, Ratio: 4194304 }
	Aspect{ Width: 16777216, Height: 1, Ratio: 16777216 }
}

func check(e error, msg string) {
    if e != nil {
        panic(msg)
    }
}

// region draw

func findAspectRatio(width int64, height int64) {
	imageAR := width / height
	distance := totalColors
	for i, ar := range aspectRatios {
		if (math.Abs(imageAR - ar.Ratio) < distance) {
			distance = ar.Ratio
			aspectRatio = ar
		}
	}
}

func readInFile(fileName string) {
	inFile, err := os.Open(fileName)
	check(err, "Error reading source file")
	srcFile = inFile
}

func createOutFile(fileName string) {
	outFile, err := os.Create(fileName)
	check(err, "Error creating output file")
	destFile = outFile
}

// func createImage(width int, height int, background color.RGBA) *image.RGBA {
//     rect := image.Rect(0, 0, width, height)
//     img := image.NewRGBA(rect)
//     draw.Draw(img, img.Bounds(), &image.Uniform{background}, image.ZP, draw.Src)
//     return img
// }

// endregion draw

// region db
func prepDBFilesystem() {
	homeFolder, err := homedir.Dir()
	check(err, "Cannot detect homedir")
	os.Chdir(homeFolder)
	storagePath = path.Join(homeFolder, ".allrgb")
	dbPath = path.Join(storagePath, dbFileName)
	backupPath = path.Join(storagePath, backupDbFileName)
	// check folder exists
	if _, existErr := os.Stat(storagePath); os.IsNotExist(existErr) {
		fmt.Println("Creating storage dir:", storagePath)
		mkdirErr := os.Mkdir(".allrgb", 0755)
		check(mkdirErr, "Cannot create storage dir")
	}
	// check db file exists
	if cleanDB {
		if _, dbExistErr := os.Stat(dbPath); !os.IsNotExist(dbExistErr) {
			fmt.Println("Deleting DB file…")
			dbRmErr := os.Remove(dbPath)
			check(dbRmErr, "Error removing DB")
		}
		if _, backupExistErr := os.Stat(backupPath); !os.IsNotExist(backupExistErr) {
			fmt.Println("Deleting DB backup file…")
			backupRmErr := os.Remove(backupPath)
			check(backupRmErr, "Error removing DB Backup")
		}
	}
	if _, dbExistErr := os.Stat(dbPath); os.IsNotExist(dbExistErr) {
		fmt.Println("Creating db file:", dbPath)
		_, touchErr := os.Create(dbPath)
		check(touchErr, "Error creating db file")
	}
}

func createTable() {
	fmt.Println("Creating fresh colors table…")
	statement, _ := db.Prepare("DROP TABLE colors")
	statement, _ = db.Prepare(`CREATE TABLE colors(
	id UNSIGNED INT AUTO_INCREMENT PRIMARY KEY,
	r UNSIGNED TINYINT NOT NULL,
	g UNSIGNED TINYINT NOT NULL,
	b UNSIGNED TINYINT NOT NULL,
	lum UNSIGED TINYINT GENERATED ALWAYS AS (ROUND((r * 0.3) + (g * 0.59) + (b * 0.11), 0)) STORED
)`)
	statement.Exec()
	statement, _ = db.Prepare("CREATE INDEX color_lum ON colors(lum)")
	statement.Exec()
}

// TODO zip backup before save
func createBackup() {
	fmt.Println("Creating DB backup", backupPath, "…")
	in, err := os.Open(dbPath)
	check(err, "Error opening DB File")
	defer in.Close()
	out, err := os.Create(backupPath)
    check(err, "Error creating DB Backup File")
    defer out.Close()
    _, err = io.Copy(out, in)
    check(err, "Error copying DB File to Backup")
}

// TODO unzip backup before copy
func createDbFromBackup() {
	fmt.Println("Creating DB from backup…")
	in, err := os.Open(backupPath)
	check(err, "Error opening DB Backup File")
	defer in.Close()
	out, err := os.Create(dbPath)
    check(err, "Error creating DB File")
    defer out.Close()
    _, err = io.Copy(out, in)
    check(err, "Error copying Backup File to DB")
}

func fillTable() {
	fmt.Println("Filling colors table…")
	deleteStmt, _ := db.Prepare("DELETE FROM colors")
	deleteStmt.Exec()
	var red uint16 = 0
	var blue uint16 = 0
	var green uint16 = 0
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
		blue = 0
		for green < 256 {
			blue = 0
			vals = make([]string, 0, 256)
			valArgs = make([]interface{}, 0, 256 * 3)
			for blue < 256 {
				vals = append(vals, "(?, ?, ?)")
				valArgs = append(valArgs, red)
				valArgs = append(valArgs, green)
				valArgs = append(valArgs, blue)
				blue++
				total++
				if total > 1 && total % 1000000 == 0 {
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
	fmt.Printf("%08d colors made in %d seconds\n", total, time.Now().Unix() - start)
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

func doesBackupExist() bool {
	_, existErr := os.Stat(backupPath)
	return !os.IsNotExist(existErr)
}

func buildDB() {
	fmt.Println("Running <db>…")
	prepDBFilesystem()
	db, _ = sql.Open("sqlite3", dbPath)
	defer db.Close()
	if !doesTableExist() {
		if doesBackupExist() {
			createDbFromBackup()
		} else {
			createTable()
		}
	}
	if !isTableFull() {
		fillTable()
		if !doesBackupExist() {
			createBackup()
		}
	}
	fmt.Println("DB ready…")
}

func drawImage() {
	fmt.Println("Running <draw>…")
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
		if len(dbArgs) == 1 && dbArgs[0] == "clean" {
			cleanDB = true
		}
		buildDB()
		os.Exit(0)
	case "draw":
		drawArgs := args[1:]
		if len(drawArgs) == 1 {
			panic("Missing output file")
		}
		if len(drawArgs) == 0 {
			panic("Missing source file")
		}
		readInFile(drawArgs[0])
		createOutFile(drawArgs[1])
		if len(drawArgs) > 2 {
			ditherVal, ditherErr := strconv.ParseUint(drawArgs[2], 10, 8);
			if ditherErr != nil || ditherVal > 3 {
				panic("Invalid dither value")
			}
			dithering = uint8(ditherVal)
			align = Center
			if len(drawArgs) > 3 {
				alignVal, alignErr := strconv.ParseInt(drawArgs[3], 10, 8);
				if alignErr != nil || alignVal > 1 || alignVal < -1 {
					panic("Invalid align value")
				}
				switch (int8(alignVal)) {
				case -1:
					align = Start
				case 0:
					align = Center
				case 1:
					align = End
				}
			}
		}
		drawImage()
		os.Exit(0)
	default:
		printHelpMenu()
		os.Exit(0)
	}
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
    dithering:  optional dithering setting (default 0);

Examples
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
./allrgb help
./allrgb db ~/.mydb
./allrgb draw input/file.png output/file.png 3
`)
}
//endregion help
package main

// TODO make sure delete db file when finished making image

import (
	"fmt"
	"math"
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
   	"golang.org/x/text/language"
	"golang.org/x/text/message"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mitchellh/go-homedir"
)

// func createImage(width int, height int, background color.RGBA) *image.RGBA {
//     rect := image.Rect(0, 0, width, height)
//     img := image.NewRGBA(rect)
//     draw.Draw(img, img.Bounds(), &image.Uniform{background}, image.ZP, draw.Src)
//     return img
// }

var dbFileName = "allrgb.db"
var backupDbFileName = "allrgb.db.backup"
var srcFile string
var outputFile string
var dithering int8
var storagePath string
var dbPath string
var backupPath string;
var db *sql.DB
var totalColors int64 = 16777216;

func check(e error, msg string) {
    if e != nil {
        panic(msg)
    }
}

func prepDBFilesystem() {
	homeFolder, err := homedir.Dir()
	check(err, "Cannot detect homedir")
	os.Chdir(homeFolder)
	storagePath = path.Join(homeFolder, ".allrgb")
	// check folder exists
	if _, existErr := os.Stat(storagePath); os.IsNotExist(existErr) {
		fmt.Println("Creating storage dir:", storagePath)
		mkdirErr := os.Mkdir(".allrgb", 0755)
		check(mkdirErr, "Cannot create storage dir")
	}
	// check db file exists
	dbPath = path.Join(storagePath, dbFileName)
	if _, dbExistErr := os.Stat(dbPath); os.IsNotExist(dbExistErr) {
		fmt.Println("Creating db file:", dbPath)
		_, touchErr := os.Create(dbPath)
		check(touchErr, "Error creating db file")
	}
}

func createTable() {
	fmt.Println("Creating colors table")
	statement, _ := db.Prepare(`CREATE TABLE IF NOT EXISTS colors(
	id UNSIGNED INT AUTO_INCREMENT PRIMARY KEY,
	r UNSIGNED TINYINT NOT NULL,
	g UNSIGNED TINYINT NOT NULL,
	b UNSIGNED TINYINT NOT NULL,
	lum UNSIGNED TINYINT NOT NULL
)`)
	statement.Exec()
	statement, _ = db.Prepare("CREATE INDEX color_lum ON colors(lum)")
	statement.Exec()
}

// TODO zip backup before save
func createBackup() {
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
	in, err := os.Open(backupPath)
	check(err, "Error opening DB Backup File")
	defer in.Close()
	out, err := os.Create(dbPath)
    check(err, "Error creating DB File")
    defer out.Close()
    _, err = io.Copy(out, in)
    check(err, "Error copying Backup File to DB")
}

 func rgb2lum(r uint16, g uint16, b uint16) uint16 {
	return uint16(math.Round((float64(r) * 0.3) + (float64(g) * 0.59) + (float64(b) * 0.11)))
}

func fillTable() {
	p := message.NewPrinter(language.English)
	fmt.Println("Deleting colors...")
	deleteStmt, _ := db.Prepare("DELETE FROM colors")
	deleteStmt.Exec()
	var red uint16 = 0
	var blue uint16 = 0
	var green uint16 = 0
	var lum uint16
	var vals []string
	var valArgs []interface{}
	total := 0
	dot := ""
	formattedTotal := ""
	fmt.Println("Generating colors...")
	for red < 256 {
		green = 0
		blue = 0
		for green < 256 {
			blue = 0
			vals = make([]string, 0, 256)
			valArgs = make([]interface{}, 0, 256 * 4)
			for blue < 256 {
				vals = append(vals, "(?, ?, ?, ?)")
				lum = rgb2lum(red, green, blue)
				valArgs = append(valArgs, red)
				valArgs = append(valArgs, green)
				valArgs = append(valArgs, blue)
				valArgs = append(valArgs, lum)
				blue++
				total++
				if total > 1 && total % 1000000 == 0 {
					dot = dot + "."
					formattedTotal = fmt.Sprintf("%10v", p.Sprintf("%d", total))
					p.Printf("%s colors made %s\n", formattedTotal, dot)
				}
			}
			insertStmt := fmt.Sprintf("INSERT INTO colors (r, g, b, lum) VALUES %s", strings.Join(vals, ","))
			_, err := db.Exec(insertStmt, valArgs...)
			check(err, "Error inserting into db")
			green++
		}
		red++
	}
	fmt.Printf("%08d colors made %s\n", total, dot)
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
	backupPath = path.Join(storagePath, backupDbFileName)
	_, existErr := os.Stat(backupPath)
	return !os.IsNotExist(existErr)
}

func buildDB() {
	fmt.Println("--DB--")
	prepDBFilesystem()
	db, _ = sql.Open("sqlite3", dbPath)
	defer db.Close()
	if !doesTableExist() {
		if doesBackupExist() {
			createDbFromBackup()
		} else {
			createTable()
			createBackup()
		}
	}
	if !isTableFull() {
		fillTable()
	}
	fmt.Println("DB Ready...")
}

func drawImage() {
	fmt.Println("--DRAW--")
}

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
	args := os.Args[1:]
	command := "help"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "db":
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
		srcFile = drawArgs[0]
		outputFile = drawArgs[1]
		if len(drawArgs) == 3 {
			ditherVal, ditherErr := strconv.ParseUint(drawArgs[2], 10, 8);
			if ditherErr != nil || ditherVal > 3 {
				panic("Invalid dither value")
			}
			dithering = int8(ditherVal)
		} else {
			dithering = 0
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
package main

import (
	"fmt"
	"github.com/nfnt/resize"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
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

// RGBColor color
type RGBColor struct {
	R uint8
	G uint8
	B uint8
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

var data map[int][]RGBColor
var keys []int
var destFileName string
var p *message.Printer
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

func drawImage(srcFile *os.File, destFile *os.File, spread int, align Alignment) {
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
	fmt.Println("ready to convert")
	convertImage(sImage, destImage, int(spread))
	fmt.Println("encoding")
	png.Encode(destFile, destImage)
	destFile.Close()
	err := os.Rename(destFileName+".part", destFileName)
	check(err, "Error saving to file")
}

func convertImage(srcImage *image.RGBA, destImage *image.RGBA, spread int) {
	passComplete := "\n************** Pass %d of %d completed (time elapsed: %d) **************\n"
	start := time.Now().Unix()
	width := destImage.Bounds().Max.X
	height := destImage.Bounds().Max.Y
	passCount := int(spread * spread)
	pass := 0
	xOffset := 0
	yOffset := 0
	x := 0
	y := 0
	for pass < passCount {
		xOffset = xOffsets[spread-1][pass]
		yOffset = yOffsets[spread-1][pass]
		// fmt.Println("xoff:", xOffset, "yoff:", yOffset, "spread:", spread, "pass:", pass)
		for y = yOffset; y < height; y += spread {
			for x = xOffset; x < width; x += spread {
				setColor(srcImage, destImage, x, y)
			}
		}
		fmt.Printf(passComplete, pass+1, passCount, time.Now().Unix()-start)
		pass++
	}
	fmt.Println("Finished converting image, duration:", time.Now().Unix()-start)
}

func setColor(srcImage *image.RGBA, destImage *image.RGBA, x int, y int) {
	srcColor := srcImage.RGBAAt(x, y)
	destColor := matchColor(srcColor)
	destImage.SetRGBA(x, y, destColor)
}

func matchColor(srcColor color.RGBA) color.RGBA {
	lum := getLum(uint16(srcColor.R), uint16(srcColor.G), uint16(srcColor.B))
	closest := findClosest(lum)
	col := getValue(closest)
	return color.RGBA{col.R, col.G, col.B, 255}
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

// region data
func getLum(r uint16, g uint16, b uint16) int {
	return int(math.Round((float64(r) * 0.3) + (float64(g) * 0.59) + (float64(b) * 0.11)))
}

func makeColor(r uint8, g uint8, b uint8) RGBColor {
	return RGBColor{R: r, G: g, B: b}
}

func lumExists(lum int) bool {
	for i := 0; i < len(keys); i++ {
		if keys[i] == lum {
			return true
		}
	}
	return false
}

func findClosest(lum int) int {
	closest := 999.99
	var val int
	for i := 0; i < len(keys); i++ {
		dist := math.Abs(float64(keys[i]) - float64(lum))
		if keys[i] == lum {
			return lum
		}
		if dist < closest {
			closest = dist
			val = keys[i]
		}
	}
	return val
}

func removeKey(lum int) {
	keyLen := len(keys)
	k := make([]int, 0)
	for i := 0; i < keyLen; i++ {
		if keys[i] != lum {
			k = append(k, keys[i])
		}
	}
	keys = k
}

func getValue(lum int) RGBColor {
	colorLen := len(data[lum])
	val := data[lum][0]
	if colorLen == 1 {
		delete(data, lum)
		removeKey(lum)
	} else {
		data[lum] = data[lum][1:]
	}
	return val
}

func generateData() {
	data = make(map[int][]RGBColor)
	var red uint16
	var green uint16
	var blue uint16
	var lum int
	for red = 0; red <= 255; red++ {
		for green = 0; green <= 255; green++ {
			for blue = 0; blue <= 255; blue++ {
				lum = getLum(red, green, blue)
				data[lum] = append(data[lum], makeColor(uint8(red), uint8(green), uint8(blue)))
			}
		}
	}

	for k := range data {
		keys = append(keys, k)
	}
	sort.Ints(keys)
}

// endregion data

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
		spread := 0
		align := Center
		if len(drawArgs) > 2 {
			spreadVal, spreadErr := strconv.ParseUint(drawArgs[2], 10, 8)
			if spreadErr != nil || spreadVal > 3 {
				panic("Invalid spread value")
			}
			spread = int(spreadVal) + 1
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
		generateData()
		drawImage(srcFile, destFile, spread, align)
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
draw: draw image
    <draw> arguments
    ┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈┈
    sourceFile: source file to draw image from
    outputFile: file name of output
    spread:  px to skip, allows better color allocation across entire image (default 0)
	    valid values are 0, 1, 2, 3 & generate corresponding px grids of 1, 4, 9, 16
    align:  optional alignment if dimensions don't match after resize (default 0)
      -1: align with start (left or top)
      0: align in center
	  1: alight with end (right or bottom)

Examples
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
./allrgb help
./allrgb draw input/file.png output/file.png 3 0`)
}

//endregion help

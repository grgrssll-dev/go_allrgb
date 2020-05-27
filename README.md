# go allrgb generator
Generates an ALLRGB image by taking a source image and converting the pixels to use every RGB color combo (16,777,216 colors) once and only once.

[Read about them and see some other people's examples here](https://allrgb.com/)
*NOTE none of the images on the linked site were generated with this software... yet? (as of 2020-05-25) but I plan on submitting one*

## Usage

### Commands
`help` print help menu

`draw` draw image

#### \<draw> arguments
* `sourceFile` source file to draw image from
* `outputFile` file name of output
* `spread`  px spread, allows better color allocation across entire image (default 0)
    valid values are 0, 1, 2, 3 & generate corresponding px grids of 1, 4, 9, 16
* `align`  optional alignment if src dimensions don't match acceptable resolution after resize (default 0)
   -1: align with start (left or top)
	0: align in center
	1: alight with end (right or bottom)
* `shuffle` default 0
	  set to 1 to shuffle colors before use (reduces banding, but makes more grey)

### Build
$`go build allrgb.go`

### Examples
$`./allrgb help`

$`./allrgb draw input/file.png output/file.png 1 -1` 2x2 px spread, align to start if not correct aspect ratio

$`./allrgb draw input/file.png output/file.png 3 0` 16x16 px spread, align center if not correct aspect ratio

$`./allrgb draw input/file.png output/file.png 3 0 1` adds shuffle param

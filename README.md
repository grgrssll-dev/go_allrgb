# go allrgb generator
Generates an ALLRGB image ([read about them and see some other people's examples here](https://allrgb.com/)) by taking a source image and converting the pixels to use every RGB color combo (16,777,216 colors) once and only once. It does this in a kinda dumb way of storing every combination of RGB in a sqlite db and matching the source pixel by luminosity.

It's not a fast process.

Why? bc I wanted to play with golang and rewriting an [old php script](https://github.com/grgrssll/allrgb-image-converter) seemed like a fun project. I'm sure there are way better ways of doing this, but I don't know them...

## Usage

### Commands
`help` print help menu

`db` prepare database

`draw` draw image (will prepare db if needed)

NOTE: db stored in `~/.allrgb/allrgb.db` (and a backup `allrgb.db.backup` for quick regeneration) size is ~700mb so remove after if want space back (zipping backup is on my todo list)

#### \<db> arguments
* `force` force regeneration of db (leave off if you don't want to force db regeneration)

#### \<draw> arguments
* `sourceFile` source file to draw image from
* `outputFile` file name of output
* `gridSize`  px to skip, allows better color allocation across entire image (default 0)
    valid values are 0, 1, 2, 3 & generate corresponding px grids of 1, 4, 9, 16
* `align`  optional alignment if src dimensions don't match acceptable resolution after resize (default 0)
   -1: align with start (left or top)
	0: align in center
	1: alight with end (right or bottom)

### Build
$`go build allrgb.go`

### Examples
`./allrgb help`

`./allrgb db` # builds db if needed

`./allrgb db force` # force rebuilds db

`./allrgb draw input/file.png output/file.png 3 0` # draw will rebuild db if needed

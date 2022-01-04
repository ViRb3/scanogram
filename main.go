package main

import (
	"errors"
	"flag"
	"fmt"
	heicexif "github.com/dsoprea/go-heic-exif-extractor"
	jpegstructure "github.com/dsoprea/go-jpeg-image-structure"
	pngstructure "github.com/dsoprea/go-png-image-structure"
	tiffstructure "github.com/dsoprea/go-tiff-image-structure"
	riimage "github.com/dsoprea/go-utility/image"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var scanPath string
var moveBadPath string
var processHidden bool
var logJson bool

func main() {
	flag.Usage = func() {
		fmt.Print(`
imgdiag: parse images in a directory to diagnose whether they are corrupted
Usage:
	imgdiag -path SCAN_DIR [OPTIONS]
Options:
`[1:])
		flag.PrintDefaults()
	}

	flag.StringVar(&scanPath, "path", "", "REQUIRED. Path to directory to scan")
	flag.StringVar(&moveBadPath, "move", "", "Move bad files to this directory")
	flag.BoolVar(&processHidden, "hidden", false, "Whether to process hidden files and directories")
	flag.BoolVar(&logJson, "json", false, "Whether to log in JSON instead of pretty print")
	flag.Parse()

	if scanPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	if !logJson {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	}
	if moveBadPath != "" {
		absMoveBadPath, err := filepath.Abs(moveBadPath)
		if err != nil {
			log.Fatal().Err(err).Msg("fatal error")
		}
		moveBadPath = absMoveBadPath
		log.Info().Str("path", moveBadPath).Msg("Will move bad files")
	}
	if processHidden {
		log.Info().Msg("Will process hidden files and directories")
	}

	log.Info().Str("path", scanPath).Msg("Scanning...")
	if err := work(); err != nil {
		log.Fatal().Err(err).Msg("fatal error")
	}
	log.Info().Msg("Done!")
}

type FileParser interface {
	ParseFile(filepath string) (ec riimage.MediaContext, err error)
}

func moveBadFile(path string) error {
	newPath := filepath.Join(moveBadPath, filepath.Base(path))
	for i := 0; ; i++ {
		var suffix string
		if i == 0 {
			suffix = ""
		} else {
			suffix = fmt.Sprintf(".%d", i)
		}
		if _, err := os.Stat(newPath + suffix); os.IsNotExist(err) {
			return os.Rename(path, newPath+suffix)
		} else if err != nil {
			return err
		}
	}
}

func work() error {
	testPaths := map[string]string{"scan path": scanPath}
	if moveBadPath != "" {
		testPaths["move path"] = moveBadPath
	}
	for name, path := range testPaths {
		if stat, err := os.Stat(path); err != nil {
			if err != nil {
				return errors.New(name + " error: " + err.Error())
			}
		} else if !stat.IsDir() {
			return errors.New(name + " is not a directory")
		}
	}

	if err := filepath.Walk(scanPath, func(path string, d fs.FileInfo, err error) error {
		pathBase := filepath.Base(path)
		if !processHidden && (strings.HasPrefix(pathBase, ".") || pathBase == "$RECYCLE.BIN") {
			if d.IsDir() {
				return fs.SkipDir
			} else {
				return nil
			}
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if absPath == moveBadPath {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		var parser FileParser
		var detectedType string
		switch filepath.Ext(path) {
		case ".jpg":
			fallthrough
		case ".jpeg":
			parser = jpegstructure.NewJpegMediaParser()
			detectedType = "JPEG"
		case ".tif":
			fallthrough
		case ".tiff":
			parser = tiffstructure.NewTiffMediaParser()
			detectedType = "TIFF"
		case ".png":
			parser = pngstructure.NewPngMediaParser()
			detectedType = "PNG"
		case ".heic":
			fallthrough
		case ".heif":
			parser = heicexif.NewHeicExifMediaParser()
			detectedType = "HEIC"
		default:
			return nil
		}
		if d.Size() < 3 {
			log.Error().Str("path", path).Int64("size", d.Size()).Msg("file too small")
			if moveBadPath != "" {
				if err := moveBadFile(path); err != nil {
					return err
				}
			}
			return nil
		}
		if _, err := parser.ParseFile(path); err != nil {
			log.Error().Str("path", path).Str("type", detectedType).Err(err).Msg("failed to parse")
			if moveBadPath != "" {
				if err := moveBadFile(path); err != nil {
					return err
				}
			}
			return nil
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

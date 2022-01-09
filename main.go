package main

import (
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/dsoprea/go-exif/v2"
	exifcommon "github.com/dsoprea/go-exif/v2/common"
	heicexif "github.com/dsoprea/go-heic-exif-extractor"
	jpegstructure "github.com/dsoprea/go-jpeg-image-structure"
	pngstructure "github.com/dsoprea/go-png-image-structure"
	tiffstructure "github.com/dsoprea/go-tiff-image-structure"
	riimage "github.com/dsoprea/go-utility/image"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var CLI struct {
	ScanPath     string `arg:"" help:"Scan images in this directory." type:"existingdir"`
	InvalidPath  string `short:"i" help:"Move invalid (corrupt) images to this directory." type:"existingdir"`
	SortPath     string `short:"s" help:"Sort and move images to this directory." type:"existingdir"`
	SortSeparate bool   `default:"false" help:"Sort EXIF and mod time separately."`
	Hidden       bool   `default:"false" help:"Process hidden files and directories."`
	Json         bool   `default:"false" help:"Log in JSON instead of pretty printing."`
	Verbose      bool   `short:"v" default:"false" help:"Verbose logging."`
	LogFile      string `default:"scanogram.log" help:"Verbose log file location. Set to empty to disable."`
}

type LevelWriter struct {
	io.Writer
	level zerolog.Level
}

func NewLevelWriter(writer io.Writer, level zerolog.Level) *LevelWriter {
	return &LevelWriter{
		Writer: writer,
		level:  level,
	}
}

func (w *LevelWriter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	if level < w.level {
		return len(p), nil
	}
	return w.Write(p)
}

func main() {
	kong.Parse(&CLI, kong.Description("Scan your images for problems and sort everything by date."))

	var logWriters []io.Writer
	if CLI.LogFile != "" {
		safeLogFilePath, err := getFileNameSafe(CLI.LogFile)
		if err != nil {
			log.Fatal().Err(err).Str("path", CLI.LogFile).Msg("parse log file location")
		}
		CLI.LogFile = safeLogFilePath
		logFile, err := os.Create(CLI.LogFile)
		if err != nil {
			log.Fatal().Err(err).Str("path", CLI.LogFile).Msg("create log file")
		}
		logWriters = append(logWriters, NewLevelWriter(logFile, zerolog.DebugLevel))
	}
	var consoleLogLevel zerolog.Level
	if CLI.Verbose {
		consoleLogLevel = zerolog.DebugLevel
	} else {
		consoleLogLevel = zerolog.InfoLevel
	}
	var consoleWriter io.Writer
	if CLI.Json {
		consoleWriter = os.Stdout
	} else {
		consoleWriter = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	logWriters = append(logWriters, NewLevelWriter(consoleWriter, consoleLogLevel))
	log.Logger = zerolog.New(zerolog.MultiLevelWriter(logWriters...))

	if CLI.InvalidPath != "" {
		log.Info().Str("path", CLI.InvalidPath).Msg("Will move invalid images")
	}
	if CLI.SortPath != "" {
		log.Info().Str("path", CLI.SortPath).Msg("Will sort images")
	}
	if CLI.Hidden {
		log.Info().Msg("Will process hidden files and directories")
	}
	if CLI.SortSeparate {
		log.Info().Msg("Will sort EXIF and mod time separately")
	}

	log.Info().Str("path", CLI.ScanPath).Msg("Scanning...")
	if err := doScan(); err != nil {
		log.Error().Err(err).Msg("fatal error")
	}
	log.Info().Msg("Done!")
}

func (f *FileProcessor) moveInvalidFileSafe(path string) error {
	return f.moveFileSafe(path, filepath.Join(CLI.InvalidPath, filepath.Base(path)))
}

// Moves a file to a new path without replacing any existing files.
// Check getFileNameSafe.
func (f *FileProcessor) moveFileSafe(path string, newPath string) error {
	newSafePath, err := getFileNameSafe(newPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newSafePath), 0755); err != nil {
		return errors.WithMessage(err, "make new path")
	}
	f.log.Debug().Str("dest", newSafePath).Msg("moving file")
	return os.Rename(path, newSafePath)
}

// Generates a new path that will not point to any existing file.
// The new file name will be suffixed with a number if necessary.
func getFileNameSafe(newPath string) (string, error) {
	for i := 0; ; i++ {
		var suffix string
		if i == 0 {
			suffix = ""
		} else {
			suffix = fmt.Sprintf("_%d", i)
		}
		dir := filepath.Dir(newPath)
		base := filepath.Base(newPath)
		extension := filepath.Ext(newPath)
		newSafePath := filepath.Join(dir, base[:len(base)-len(extension)]) + suffix + extension
		if _, err := os.Stat(newSafePath); os.IsNotExist(err) {
			return newSafePath, nil
		} else if err != nil {
			return "", errors.WithMessage(err, "stat new path")
		}
	}
}

func doScan() error {
	if err := filepath.Walk(CLI.ScanPath, func(path string, d fs.FileInfo, err error) error {
		logger := log.With().Str("path", path).Int64("size", d.Size()).Logger()
		if err := NewFileProcessor(logger, path, d).Run(); errors.Is(err, fs.SkipDir) {
			return fs.SkipDir
		} else if err != nil {
			logger.Err(err).Msg("file error")
		}
		return nil
	}); err != nil {
		return errors.WithMessage(err, "walk")
	}
	return nil
}

func NewFileProcessor(logger zerolog.Logger, path string, d fs.FileInfo) *FileProcessor {
	return &FileProcessor{logger, path, d}
}

type FileProcessor struct {
	log  zerolog.Logger
	path string
	d    fs.FileInfo
}

type FileParser interface {
	ParseFile(filepath string) (ec riimage.MediaContext, err error)
}

func (f *FileProcessor) Run() error {
	pathBase := filepath.Base(f.path)
	if !CLI.Hidden && (strings.HasPrefix(pathBase, ".") || pathBase == "$RECYCLE.BIN") {
		f.log.Debug().Msg("skipping hidden file")
		if f.d.IsDir() {
			return fs.SkipDir
		} else {
			return nil
		}
	}
	absPath, err := filepath.Abs(f.path)
	if err != nil {
		return errors.WithMessage(err, "abs file path")
	}
	if absPath == CLI.InvalidPath || absPath == CLI.SortPath {
		f.log.Debug().Msg("skipping special directory")
		return fs.SkipDir
	}
	if f.d.IsDir() {
		return nil
	}
	parser, detectedType := getFileParser(f.path)
	if detectedType == "" {
		f.log.Debug().Msg("failed to detect file type")
		return nil
	}
	if f.d.Size() < 3 {
		if CLI.InvalidPath != "" {
			if err := f.moveInvalidFileSafe(f.path); err != nil {
				return errors.WithMessage(err, "move invalid file")
			}
		}
		return errors.New("file too small")
	}
	parsedFile, err := parser.ParseFile(f.path)
	if err != nil {
		if CLI.InvalidPath != "" {
			if err := f.moveInvalidFileSafe(f.path); err != nil {
				return errors.WithMessage(err, "move invalid file")
			}
		}
		return errors.New("failed to parse")
	}
	if CLI.SortPath != "" {
		if err := f.sort(parsedFile); err != nil {
			return errors.WithMessage(err, "sort")
		}
	}
	return nil
}

func (f *FileProcessor) sort(parsedFile riimage.MediaContext) error {
	rootIfd, _, err := parsedFile.Exif()
	var date *time.Time
	var model string
	if err == nil {
		date, err = getDate(rootIfd)
		if err != nil {
			return errors.WithMessage(err, "get date")
		}
		model, err = getModel(rootIfd)
		if err != nil {
			return errors.WithMessage(err, "get model")
		}
	}
	usedModTime := false
	if date == nil || date.Year() == -1 {
		f.log.Debug().Msg("missing EXIF date")
		usedModTime = true
		modTime := f.d.ModTime()
		date = &modTime
	}
	if model == "" {
		f.log.Debug().Msg("missing EXIF model")
		model = "Unknown Device"
	}
	separateDir := ""
	if CLI.SortSeparate {
		if usedModTime {
			separateDir = "MOD_TIME"
		} else {
			separateDir = "EXIF"
		}
	}
	if err := f.moveFileSafe(f.path, filepath.Join(
		CLI.SortPath,
		separateDir,
		fmt.Sprintf("%02d", date.Year()),
		fmt.Sprintf("%02d", date.Month()),
		cleanFileName(model),
		fmt.Sprintf("%04d_%02d_%02d", date.Year(), date.Month(), date.Day())+filepath.Ext(f.path)),
	); err != nil {
		return errors.WithMessage(err, "move file")
	}
	return nil
}

type SearchItem struct {
	*exif.Ifd
	string
}

// Returns the IFD's date as a *time.Time, or nil if it does not exist.
func getDate(rootIfd *exif.Ifd) (*time.Time, error) {
	var searchList []SearchItem
	exifIfd, err := rootIfd.ChildWithIfdPath(exifcommon.IfdExifStandardIfdIdentity)
	if err == nil {
		searchList = append(searchList, SearchItem{exifIfd, "DateTimeOriginal"})
	} else if !errors.Is(err, exif.ErrTagNotFound) {
		return nil, err
	}
	searchList = append(searchList, SearchItem{rootIfd, "DateTime"})
	for _, item := range searchList {
		value, err := getTagString(item.Ifd, item.string)
		if err != nil {
			return nil, err
		}
		if value != "" {
			date, err := exif.ParseExifFullTimestamp(value)
			return &date, err
		}
	}
	return nil, nil
}

// Strips any non-ASCII characters from the input string.
func cleanText(input string) string {
	return strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || r == 0 {
			return -1
		}
		return r
	}, strings.TrimSpace(input))
}

// Strips any non-filename characters from the input string.
func cleanFileName(input string) string {
	var invalidFilenameChars = regexp.MustCompile(`[/\\?%*:|"<>]`)
	return invalidFilenameChars.ReplaceAllLiteralString(strings.TrimSpace(input), "")
}

// Returns the IFD's model tag as a string, or an empty string if it does not exist.
func getModel(rootIfd *exif.Ifd) (string, error) {
	exifMake, err := getTagString(rootIfd, "Make")
	if err != nil {
		return "", err
	}
	exifModel, err := getTagString(rootIfd, "Model")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(exifMake + " " + exifModel), nil
}

// Returns the tag's value as a string, or an empty string if it does not exist.
func getTagString(rootIfd *exif.Ifd, tagName string) (string, error) {
	tags, err := rootIfd.FindTagWithName(tagName)
	if errors.Is(err, exif.ErrTagNotFound) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if len(tags) < 1 {
		return "", nil
	} else if len(tags) > 1 {
		return "", errors.New("more than one EXIF tag matched for " + tagName)
	}
	value, err := tags[0].Value()
	if err != nil {
		return "", err
	}
	return cleanText(value.(string)), nil
}

// Returns a FileParser for the input based on its file extension.
func getFileParser(path string) (FileParser, string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg":
		fallthrough
	case ".jpeg":
		return jpegstructure.NewJpegMediaParser(), "JPEG"
	case ".tif":
		fallthrough
	case ".tiff":
		return tiffstructure.NewTiffMediaParser(), "TIFF"
	case ".png":
		return pngstructure.NewPngMediaParser(), "PNG"
	case ".heic":
		fallthrough
	case ".heif":
		return heicexif.NewHeicExifMediaParser(), "HEIC"
	default:
		return nil, ""
	}
}

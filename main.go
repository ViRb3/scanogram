package main

import (
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/barasher/go-exiftool"
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
	ScanPath     string   `arg:"" help:"Scan files in this directory." type:"existingdir"`
	ScanExts     []string `default:"jpg,jpeg,tif,tiff,png,heic,heif,bmp,mp4,mov,mkv,avi,3gp,wmv,mpg,mpeg" help:"Scan only files with these extensions."`
	InvalidPath  string   `short:"i" help:"Move invalid (corrupt) files to this directory." type:"existingdir"`
	SortPath     string   `short:"s" help:"Sort and move files to this directory." type:"existingdir"`
	SortSeparate bool     `default:"false" help:"Sort EXIF and mod time in separate folders."`
	Hidden       bool     `default:"false" help:"Process hidden files and directories."`
	Json         bool     `default:"false" help:"Log in JSON instead of pretty printing."`
	Verbose      bool     `short:"v" default:"false" help:"Verbose logging."`
	LogFile      string   `default:"scanogram.log" help:"Verbose log file location. Set to empty to disable."`
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
	kong.Parse(&CLI, kong.Description("Scan your pictures and videos for corruption, and sort them by EXIF or modification time."))

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
		defer logFile.Close()
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
	log.Logger = log.Output(zerolog.MultiLevelWriter(logWriters...))

	if CLI.InvalidPath != "" {
		log.Info().Str("path", CLI.InvalidPath).Msg("Will move invalid files")
	}
	if CLI.SortPath != "" {
		log.Info().Str("path", CLI.SortPath).Msg("Will sort files")
	}
	if CLI.Hidden {
		log.Info().Msg("Will process hidden files and directories")
	}
	if CLI.SortSeparate {
		log.Info().Msg("Will sort EXIF and mod time in separate folders")
	}
	log.Info().Strs("exts", CLI.ScanExts).Msg("Will scan files with these extensions")

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
	exifTool, err := exiftool.NewExiftool()
	if err != nil {
		return errors.WithMessage(err, "init exiftool")
	}
	defer exifTool.Close()
	scanExtMap := map[string]bool{}
	for _, ext := range CLI.ScanExts {
		scanExtMap["."+ext] = true
	}
	if err := filepath.Walk(CLI.ScanPath, func(path string, d fs.FileInfo, err error) error {
		if _, ok := scanExtMap[strings.ToLower(filepath.Ext(path))]; !ok && !d.IsDir() {
			return nil
		}
		logger := log.With().Str("path", path).Int64("size", d.Size()).Logger()
		if err := NewFileProcessor(logger, path, d, exifTool).Run(); errors.Is(err, fs.SkipDir) {
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

func NewFileProcessor(logger zerolog.Logger, path string, d fs.FileInfo, exifTool *exiftool.Exiftool) *FileProcessor {
	return &FileProcessor{logger, path, d, exifTool}
}

type FileProcessor struct {
	log      zerolog.Logger
	path     string
	d        fs.FileInfo
	exifTool *exiftool.Exiftool
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
	if f.d.Size() < 3 {
		if CLI.InvalidPath != "" {
			if err := f.moveInvalidFileSafe(f.path); err != nil {
				return errors.WithMessage(err, "move invalid file")
			}
		}
		return errors.New("file too small")
	}
	parser, detectedType := getFileParser(f.path)
	if detectedType != "" {
		if _, err := parser.ParseFile(f.path); err != nil {
			if CLI.InvalidPath != "" {
				if err := f.moveInvalidFileSafe(f.path); err != nil {
					return errors.WithMessage(err, "move invalid file")
				}
			}
			return errors.WithMessage(err, "failed to parse")
		}
	}
	if CLI.SortPath != "" {
		if err := f.sort(); err != nil {
			return errors.WithMessage(err, "sort")
		}
	}
	return nil
}

func (f *FileProcessor) sort() error {
	fileInfos := f.exifTool.ExtractMetadata(f.path)
	date := f.getDate(fileInfos)
	model := f.getModel(fileInfos)
	usedModTime := false
	if date.Year() <= 1 {
		f.log.Debug().Msg("missing EXIF date")
		usedModTime = true
		modTime := f.d.ModTime()
		date = modTime
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

func (f *FileProcessor) getDate(fileInfos []exiftool.FileMetadata) time.Time {
	dateRaw := fileInfos[0].Fields["DateTimeOriginal"]
	if dateRaw == nil {
		dateRaw = fileInfos[0].Fields["DateTime"]
	}
	switch dateRaw.(type) {
	case string:
		date, err := time.Parse("2006:01:02 15:04:05", dateRaw.(string))
		if err == nil {
			return date
		}
	}
	return time.Time{}
}

func (f *FileProcessor) getModel(fileInfos []exiftool.FileMetadata) string {
	makeRaw := fileInfos[0].Fields["Make"]
	modelRaw := fileInfos[0].Fields["Model"]
	var model []string
	switch makeRaw.(type) {
	case string:
		model = append(model, makeRaw.(string))
	}
	switch modelRaw.(type) {
	case string:
		model = append(model, modelRaw.(string))
	}
	return cleanText(strings.Join(model, " "))
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

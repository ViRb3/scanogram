# scanogram

> Scan your pictures and videos for corruption, and sort them by EXIF or modification time.

## Introduction

This tool is a fast and lightweight scanner for potentially corrupted pictures and videos, for example after disaster recovery of a hard drive. It works by parsing the file structure, and not reading the entire file. This tool can also sort your multimedia files based on date, using EXIF where possible and modification time as fallback.

The following formats are supported for corruption checking:

- JPEG
- TIFF
- PNG
- HEIC

Sorting by EXIF time uses [exiftool](https://exiftool.org/), so all formats supported by the tool also work here.

Sorting by modification time works on all files.

## Usage

Make sure you have [exiftool](https://exiftool.org/) installed and added to your PATH (executable by typing `exiftool` in any Terminal).

```bash
$ ./scanogram --help
```

```
Usage: scanogram <scan-path>

Scan your images for problems and sort everything by date.

Arguments:
  <scan-path>    Scan images in this directory.

Flags:
  -h, --help                        Show context-sensitive help.
      --scan-exts=jpg,jpeg,tif,tiff,png,heic,heif,bmp,mp4,mov,mkv,avi,3gp,wmv,mpg,mpeg,...
                                    Scan only files with these extensions. Set to empty to scan all.
  -i, --invalid-path=STRING         Move invalid (corrupt) files to this directory.
  -s, --sort-path=STRING            Sort and move files to this directory.
      --sort-separate               Sort EXIF and mod time in separate folders.
      --hidden                      Process hidden files and directories.
      --json                        Log in JSON instead of pretty printing.
  -v, --verbose                     Verbose logging.
      --log-file="scanogram.log"    Verbose log file location. Set to empty to disable.
```

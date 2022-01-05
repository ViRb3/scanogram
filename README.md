# scanogram

> Scan your images for problems and sort everything by date.

## Introduction

This tool is a fast and lightweight scanner for potentially corrupted images, for example after disaster recovery of a hard drive. It works by parsing the file structure, and not reading the entire file. This tool can also sort your images based on date, using EXIF where possible and mod time as fallback.

Currently, the following formats are supported:

- JPEG
- TIFF
- PNG
- HEIC

## Usage

```bash
$ ./scanogram --help
```

```
Usage: scanogram <scan-path>

Scan your images for problems and sort everything by date.

Arguments:
  <scan-path>    Scan images in this directory.

Flags:
  -h, --help                   Show context-sensitive help.
  -i, --invalid-path=STRING    Move invalid (corrupt) images to this directory.
  -s, --sort-path=STRING       Sort and move images to this directory.
      --sort-separate          Sort EXIF and mod time separately.
      --hidden                 Process hidden files and directories.
      --json                   Log in JSON instead of pretty printing.
  -v, --verbose                Verbose logging.
```

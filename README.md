# imgdiag

> Parse images in a directory to diagnose whether they are corrupted

## Introduction

This tool is a fast and lightweight scanner for potentially corrupted images, for example after disaster recovery of a hard drive. It works by parsing the structure, but not reading the entire file. Currently, the following formats are supported:

- JPEG
- TIFF
- PNG
- HEIC

## Usage

```bash
$ imgdiag -help
```

```bash
Usage:
        imgdiag -path SCAN_DIR [OPTIONS]
Options:
  -hidden
        Whether to process hidden files and directories
  -json
        Whether to log in JSON instead of pretty print
  -move string
        Move bad files to this directory
  -path string
        REQUIRED. Path to directory to scan
```

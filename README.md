# FLAC Library Integrity Check

## Genesis

[FLAC](https://xiph.org/flac/) is an audio format suited to preserving audio quality. FLAC files can contain an MD5 hash of their decoded audio data. The FLAC command-line tool can recalculate that hash and compare it with the stored value to check the audio data's integrity.

Unfortunately, these tools don't make it easy to `test` an entire library of files. As my FLAC collection grew to more than 1000 albums, I needed a more comprehensive tool. Here's what I was looking for:

* check the integrity of new files added to a directory and its subdirectories
* check the integrity of modified files in a directory and its subdirectories
* perform regular integrity checks, starting with the oldest checked file first

Finally, I wrote a program that fulfils these needs.

This check compares the audio data with the hash stored in the same file. It is not a comparison with a separately stored historical hash, and it does not verify that metadata has remained unchanged.

## Requirements

- Go 1.27.3 or later to build the program.
- The `flac` executable; its full path is provided with `--flac`.

Build from the project directory:

```powershell
go build -o flic-go.exe .
```


## Usage

| Option                     | Description   |
| --------                   | -------- |
| `--flac <path>`       | Path to the `flac` executable (**mandatory**) |
| `--folder <path>`        | Root folder of the FLAC collection; subfolders are searched recursively (**mandatory**) |
| `--report <path>`   | Path to the report file (**mandatory**) |
| `--age <number>`   | Check files by age in minutes; `-1` selects never-checked files and `-2` selects files checked before today |
| `--min-percentage <number>`  | Minimum percentage of collection to check |
| `--max-percentage <number>`  | Maximum percentage of collection to check |
| `--max-missing-percentage <number>` | Maximum percentage of report entries allowed to be missing (default: 50) |

Use absolute paths for the executable, collection and report. For example, in PowerShell:

```powershell
.\flic-go.exe --flac="C:\Tools\flac.exe" --folder="D:\Music" --report="D:\Reports\integrity.csv" --age=-1
```

Choose `--age` or `--min-percentage` to identify files to check. `--max-percentage` can only be used with `--age`. The program normally selects the oldest checks first; when all files are selected, it checks them in path order.

`--age` can be used alone. With a non-negative value, it selects files not checked within that number of minutes. For example, use `--age=10080` for one week. `--age=-1` selects files that have never been checked, including files detected as new or modified. `--age=-2` selects files last checked before the beginning of the current local day.

`--min-percentage` can be used alone. To check at least 25% of your collection, use `--min-percentage=25`.

`--max-percentage` limits the number of files selected by `--age`; it cannot be used alone. `--min-percentage` and `--max-percentage` cannot be combined.

`--age` can be combined with either percentage option:

* With `--min-percentage`, the program checks whichever is larger: the number selected by age or the minimum percentage.
* With `--max-percentage`, it checks whichever is smaller: the number selected by age or the maximum percentage.

For example,

| Files identified by `--age`  | Files identified by `--min-percentage`  | Files to check |
| --------                     | --------                                | --------  |
| 357 files                    | **761 files**                           | **761 files** since 761 > 357 |
| **860 files**                | 252 files                               | **860 files** since 860 > 252 |


| Files identified by `--age`  | Files identified by `--max-percentage`  | Files to check |
| --------                     | --------                                | -------- |
| 593 files                    | **247 files**                           | **247 files** since 247 < 593 |
| **145 files**                | 465 files                               | **145 files** since 145 < 465 |


## Report and safety

The report is created on the first run. Its parent directory must already exist. Existing reports must contain the four columns `File`, `Size`, `Mod-Time`, and `Last-Check`; malformed reports are rejected without overwriting them. `Last-Check` is stored in the local timezone, without a timezone offset.

The program detects new or modified files using their paths, sizes and modification times, with a one-millisecond tolerance on modification times. This quick scan can miss a change if the size and modification time are preserved. Periodic checks of older files remain useful.

The report is saved every 3 minutes, after a file finishes checking, and again at the end. A temporary file is written before the report is replaced. If FLAC reports an error or cannot verify the stored MD5, the program stops at that file and saves completed checks before exiting with an error. An existing earlier check date for the failed file is currently retained.

While a report is in use, a `<report>.lock` file prevents another instance from using the same report. It records the machine, process ID, and start time. After an abrupt interruption the lock may remain; check that no process is using the report before removing it manually. This protection requires all instances to use the same lock path and a filesystem that supports exclusive file creation.

Before changing the report, the program compares its existing file paths with the scanned collection. On Windows, case differences are ignored. If multiple paths become indistinguishable, the program stops without changing the report. If more than `--max-missing-percentage` percent are missing, it stops and leaves the report unchanged. The default is 50; set it to 0 to reject any missing entry, or 100 to allow all missing entries. Missing entries within the limit are removed from the report. Renamed or moved files count as missing under their former paths.

Percentages must be between 0 and 100. Minimum percentages are rounded up to a whole file, while maximum percentages are rounded down. Exit status is 0 for success, 2 for invalid options, and 1 for other errors.

## License

This project is licensed under the terms of the **MIT license**.
See the [licence file](LICENCE.md) for more information.

# scummer

## What is it

This is a utility for automatically generating .scummvm files for certain setups such as OnionOS.

## How does it work

It uses the scummvm binary to detect the Game ID. If multiple matches are found, then the best choice is used. It then writes the Game ID to a .scummvm file.

## How to use it

Run: `scummer <scummvm binary file> <scummvm data file directory>`

`scummvm binary file` is the path to your scummvm binary. On Windows, you must be running as Administrator in order for scummer to be able to call scummvm.

`scummvm data file directory` is the location of your scummvm data files. Currently, each game must be under its own directory under this path. Scummer will scan each directory using the scummvm binary in order to detect the game. The output .scummvm files will be generated at this path.

Upon completion of the scan, both a `error.json` and `success.json` file are generated in the current working directory. `error.json` contains all the unsuccessful detections, and `success.json` contains all the successful detections.

Example usage: `scummer "C:\scummvm\scummvm.exe" "C:\scummvm\games"`

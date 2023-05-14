package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/kljensen/snowball"
)

// This is an app that takes the location of the scummvm binary file and the location
// of the scummvm data files that have been already unzipped into directories.
// The app gets a list of the directories, and runs each one through the scummvm binary
// using the "--detect" command line option. The output of the scummvm binary is then
// parsed to get the GameID. The GameID is then used to generate a .scummvm text file
// that contains just the GameID. The .scummvm file is then placed in the directory
// that contains all of the scummvm data file directories.

// There are several possibilities for the output of the scummvm binary. First, its
// possible that it can't find any games at the location given. Second, its possible
// that it can find a game and returns its GameID. Third, its possible that it can
// find a game, but it is not sure of what it is, so it returns a list of possible
// GameIDs. The app will handle each of these cases. In the third case, the app will
// stem the Description and directory name of each GameID and then compare the stemmed
// Description and directory name to see if they are similar using Levenshtein distance.
// If the stemmed Description and directory name are similar enough, then the app will
// use that GameID. If the stemmed Description and directory name are not similar
// enough, then the app will print out the GameID and the Description and directory
// name and ask the user to choose which one to use. The app will then use the chosen
// GameID. Finally, scummvm can be executed with the "--version" command line option
// to get the version of scummvm. The app will use this output as a sanity check to
// make sure that the scummvm binary can be used.

// Some sample outputs from scummvm are as follows.

// When the game cannot be found, scummvm returns:
// C:\Program Files\ScummVM>scummvm --detect --path="G:\example\SCUMMVM"
// WARNING: ScummVM could not find any game in G:\example\SCUMMVM\
// WARNING: Consider using --recursive to search inside subdirectories

// When the game can be found, scummvm returns:
// GameID                         Description                                                Full Path
// ------------------------------ ---------------------------------------------------------- ---------------------------------------------------------
// scumm:loom                     Loom (VGA/DOS/English)                                     G:\example\scummvm\Loom (CD DOS VGA)\

// When the game can be found, but scummvm is not sure of what it is, then scummvm returns:
// The game in 'Astro Chicken (Floppy DOS)\' seems to be an unknown game variant.
//
// Please report the following data to the ScummVM team at
// https://bugs.scummvm.org/ along with the name of the game you tried to add and
// its version, language, etc.:
//
// Matched game IDs for the director engine: iwave-mac
//
//   {"!", 0, "d:52807765c2438df92ebf1ab1fdbe6dfc", 1792},
//
// GameID                         Description                                                Full Path
// ------------------------------ ---------------------------------------------------------- ---------------------------------------------------------
// director:iwave                 Interactive Wave (Issue 1/Macintosh/English)               G:\example\SCUMMVM\Astro Chicken (Floppy DOS)\
// sci:astrochicken               Astro Chicken (DOS/English)                                G:\example\SCUMMVM\Astro Chicken (Floppy DOS)\

// When running scummvm with just the "--version" command line option, scummvm returns:
// ScummVM 2.7.0 (Feb 14 2023 14:26:43)
// Features compiled in: Vorbis FLAC MP3 RGB zLib MPEG2 FluidSynth Theora AAC A/52 FreeType2 FriBiDi JPEG PNG GIF taskbar TTS cloud (servers, local) TinyGL OpenGL (with shaders)

type ScummGameMatch struct {
	GameID      string `json:"GameID"`
	Description string `json:"Description"`
	Directory   string `json:"Directory"`
}

// parseScummvmOutput takes in the output of the scummvm binary and returns the GameID
// and the Description of the GameID.
func parseScummvmOutput(scummvmOutput string) (string, string, error) {
	// Check if the scummvm output contains the string "WARNING: ScummVM could not find any game in"
	if strings.Contains(scummvmOutput, "WARNING: ScummVM could not find any game in") {
		// Return an error
		return "", "", fmt.Errorf("scummvm could not find any game")
	}

	// Make sure the scummvm output contains a match for regex "GameID\s+Description\s+Full Path"
	if !regexp.MustCompile(`GameID\s+Description\s+Full Path`).MatchString(scummvmOutput) {
		// Return an error
		return "", "", fmt.Errorf("scummvm output does not contain a match for regex \"GameID\\s+Description\\s+Full Path\"")
	}

	// Define newlines for the scummvm output in case we're running on Windows
	eol := "\n"
	if strings.Contains(scummvmOutput, "\r\n") {
		eol = "\r\n"
	}

	// Split the scummvm output by newlines
	scummvmOutputSplit := strings.Split(scummvmOutput, eol)

	// Create a slice that contains a possible set of matches
	var scummvmOutputSlice []ScummGameMatch

	// Generate regex for matching the line that contains the GameID, Description, and Directory
	matcher := regexp.MustCompile(`^(.+?)\s{2,}(.+?)\s{2,}(.+?)$`)
	lineMatcher := regexp.MustCompile(`^-+\s-+\s-+$`)

	// Loop through each line of the scummvm output
	// and then find the first line that matches the regex "^-+\s-+\s-+$"
	// and then loop through each line after that line until the end of the
	// scummvm output and then parse each line into a ScummGameMatch struct
	// and then append the ScummGameMatch struct to the scummvmOutputSlice
	for i := 0; i < len(scummvmOutputSplit); i++ {
		// Check if the line matches the regex "^-+\s-+\s-+$"
		if lineMatcher.MatchString(scummvmOutputSplit[i]) {
			// Loop through each line after the line that matches the regex "^-+\s-+\s-+$"
			// until the end of the scummvm output
			for j := i + 1; j < len(scummvmOutputSplit); j++ {
				// Using the regex "^(.+)\s{2,}(.+)\s{2,}(.+)$", parse the line into
				// three groups: GameID, Description, and Directory and save them into
				// a ScummGameMatch struct
				scummGameMatch := ScummGameMatch{}
				scummGameMatch.GameID = matcher.ReplaceAllString(scummvmOutputSplit[j], "$1")
				scummGameMatch.Description = matcher.ReplaceAllString(scummvmOutputSplit[j], "$2")
				scummGameMatch.Directory = matcher.ReplaceAllString(scummvmOutputSplit[j], "$3")

				// If any of the fields in the ScummGameMatch struct are empty, then
				// continue to the next line
				if scummGameMatch.GameID == "" || scummGameMatch.Description == "" || scummGameMatch.Directory == "" {
					continue
				}

				// Append the ScummGameMatch struct to the scummvmOutputSlice
				scummvmOutputSlice = append(scummvmOutputSlice, scummGameMatch)
			}

			// Break out of the loop
			break
		}
	}

	// Check if the scummvmOutputSlice is empty
	if len(scummvmOutputSlice) == 0 {
		// Return an error
		return "", "", fmt.Errorf("scummvm output slice is empty")
	}

	// If scummvmOutputSlice only has one element, then return that element
	if len(scummvmOutputSlice) == 1 {
		return scummvmOutputSlice[0].GameID, scummvmOutputSlice[0].Description, nil
	}

	// Setup Levenshtein distance
	lev := metrics.NewLevenshtein()
	lev.CaseSensitive = false
	lev.InsertCost = 1
	lev.ReplaceCost = 2
	lev.DeleteCost = 1

	// If scummvmOutputSlice has more than one element, then interate through each element
	// and stem both the Description and Directory and then use Levenshtein distance to find
	// the closest match between Description and Directory. Then return the GameID and Description
	// of the closest match.
	closestMatchIndex := 0
	closestMatchDistance := 0.0
	for i := 0; i < len(scummvmOutputSlice); i++ {
		// Stem the GameID and Directory
		stemmedGameDescription, err := snowball.Stem(scummvmOutputSlice[i].Description, "english", false)
		if err != nil {
			continue
		}
		baseDirectory := filepath.Base(scummvmOutputSlice[i].Directory)
		stemmedDirectory, err := snowball.Stem(baseDirectory, "english", false)
		if err != nil {
			continue
		}

		// Calculate the Levenshtein distance between the stemmed GameID and Directory
		levenshteinDistance := strutil.Similarity(stemmedGameDescription, stemmedDirectory, lev)

		// Check if the levenshteinDistance is greater than the closestMatchDistance
		if levenshteinDistance > closestMatchDistance {
			// Update the closestMatchIndex and closestMatchDistance
			closestMatchIndex = i
			closestMatchDistance = levenshteinDistance
		}
	}

	// Return the closest match
	return scummvmOutputSlice[closestMatchIndex].GameID, scummvmOutputSlice[closestMatchIndex].Description, nil
}

// executeScummvmBinary takes in the location of the scummvm binary file, and a slice of
// strings that are the command line arguments to pass to the scummvm binary. The function
// executes the scummvm binary with the command line arguments and returns the output of
// the scummvm binary.
func executeScummvmBinary(scummvmBinaryFile string, commandLineArguments []string) (string, error) {
	// Create a new command
	cmd := exec.Command(scummvmBinaryFile, commandLineArguments...)
	var out bytes.Buffer
	cmd.Stdout = &out

	// Execute the command
	err := cmd.Run()
	if err != nil {
		return out.String(), err
	}

	// Return the output
	return out.String(), nil
}

// getScummvmDataFileDirectories takes in a directory path and returns a list of all the
// directories that are in the directory path.
func getScummvmDataFileDirectories(scummvmDataFileDirectory string) ([]string, error) {
	// Get a list of all the files in the directory
	files, err := os.ReadDir(scummvmDataFileDirectory)
	if err != nil {
		return nil, err
	}

	// Create a slice to store the scummvm data file directories
	scummvmDataFileDirectories := make([]string, 0)

	// Loop through each file and check if it is a directory
	for _, file := range files {
		// Check if the file is a directory
		if file.IsDir() {
			// Add the file to the list of scummvm data file directories
			scummvmDataFileDirectories = append(scummvmDataFileDirectories, file.Name())
		}
	}

	// Return the list of scummvm data file directories
	return scummvmDataFileDirectories, nil
}

func main() {
	// First check if we have at least two arguments
	if len(os.Args) < 3 {
		fmt.Println("Please provide two arguments: <scummvm binary file> <scummvm data file directory>")
		return
	}

	// Get the two arguments
	scummvmBinaryFile := os.Args[1]
	scummvmDataFileDirectory := os.Args[2]

	// Check if the first argument is a file
	if f, err := os.Stat(scummvmBinaryFile); os.IsNotExist(err) && f.IsDir() {
		fmt.Println("The first argument is not a file")
		return
	}
	// Check if the second argument is a directory
	if d, err := os.Stat(scummvmDataFileDirectory); os.IsNotExist(err) && d.IsDir() {
		fmt.Println("The second argument is not a directory")
		return
	}

	// Check if the scummvm binary file returns a version
	scummvmVersion, err := executeScummvmBinary(scummvmBinaryFile, []string{"--version"})
	if err != nil {
		fmt.Println(scummvmVersion)
		fmt.Println(err)
		return
	}
	if !strings.Contains(scummvmVersion, "ScummVM") {
		fmt.Println("The scummvm binary file is invalid")
		return
	}

	// Get a list of all the scummvm data file directories
	scummvmDataFileDirectories, err := getScummvmDataFileDirectories(scummvmDataFileDirectory)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Create a slice to hold successfully parsed ScummGameMatch structs
	scummvmOutputSlice := make([]ScummGameMatch, 0)

	// Create a slice to hold unsuccessfully parsed ScummGameMatch structs
	scummvmOutputErrorSlice := make([]ScummGameMatch, 0)

	// Loop through each scummvm data file directory
	// and execute "scummvm --detect --path=<scummvm data file directory>"
	// and then parse the output to get the GameID and Description
	for _, scummvmDataFilePath := range scummvmDataFileDirectories {
		// Join the scummvm data file directory with the scummvm data file directory path
		scummvmJoinedDataFilePath := filepath.Join(scummvmDataFileDirectory, scummvmDataFilePath)

		fmt.Printf("%s... ", scummvmJoinedDataFilePath)

		// Execute "scummvm --detect --path=<scummvm data file directory>"
		scummvmOutput, err := executeScummvmBinary(scummvmBinaryFile, []string{"--detect", "--path=" + scummvmJoinedDataFilePath})
		if err != nil {
			// Add the ScummGameMatch struct to the scummvmOutputErrorSlice
			scummvmOutputErrorSlice = append(scummvmOutputErrorSlice, ScummGameMatch{GameID: "unknown", Description: err.Error(), Directory: scummvmJoinedDataFilePath})
			fmt.Printf("❌\n")
			continue
		}

		// Parse the output
		scummvmGameID, scummvmDescription, err := parseScummvmOutput(scummvmOutput)
		if err != nil {
			// Add the ScummGameMatch struct to the scummvmOutputErrorSlice
			scummvmOutputErrorSlice = append(scummvmOutputErrorSlice, ScummGameMatch{GameID: "unknown", Description: err.Error(), Directory: scummvmJoinedDataFilePath})
			fmt.Printf("❌\n")
			continue
		}

		// Add the ScummGameMatch struct to the scummvmOutputSlice
		scummvmOutputSlice = append(scummvmOutputSlice, ScummGameMatch{GameID: scummvmGameID, Description: scummvmDescription, Directory: scummvmJoinedDataFilePath})

		fmt.Printf("✅\n")
	}

	// Save the scummvmOutputSlice to a JSON file
	scummvmOutputJSON, err := json.MarshalIndent(scummvmOutputSlice, "", "    ")
	if err != nil {
		fmt.Println(err)
		return
	}
	err = ioutil.WriteFile("success.json", scummvmOutputJSON, 0644)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Save the scummvmOutputErrorSlice to a JSON file
	scummvmOutputErrorJSON, err := json.MarshalIndent(scummvmOutputErrorSlice, "", "    ")
	if err != nil {
		fmt.Println(err)
		return
	}
	err = ioutil.WriteFile("error.json", scummvmOutputErrorJSON, 0644)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("Writing entries out to .scummvm files...")

	// Write each scummvmOutputSlice entry to a file that ends with .scummvm and contains the GameID
	for _, scummvmOutput := range scummvmOutputSlice {
		// Create the file name
		scummvmFileName := scummvmOutput.Directory + ".scummvm"

		// Create the file
		scummvmFile, err := os.Create(scummvmFileName)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer scummvmFile.Close()

		// Write the file
		_, err = scummvmFile.WriteString(scummvmOutput.GameID)
		if err != nil {
			fmt.Println(err)
			return
		}
	}

}

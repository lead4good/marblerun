package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/c2h5oh/datasize"
	"github.com/fatih/color"
	"github.com/pelletier/go-toml"
	"github.com/spf13/cobra"
)

// premainName is the name of the premain executable used.
const premainName = "premain-libos"

// uuidName is the file name of a Marble's uuid.
const uuidName = "uuid"

// commentMarbleRunAdditions holds the marker which is appended to the Gramine manifest before the performed additions.
const commentMarbleRunAdditions = "\n# MARBLERUN -- auto generated configuration entries \n"

// longDescription is the help text shown for this command.
const longDescription = `Modifies a Gramine manifest for use with MarbleRun.

This command tries to automatically adjust the required parameters in an already existing Gramine manifest template, simplifying the migration of your existing Gramine application to MarbleRun.
Please note that you still need to manually create a MarbleRun manifest.

For more information about the requirements and  changes performed, consult the documentation: https://edglss.cc/doc-mr-gramine

The parameter of this command is the path of the Gramine manifest template you want to modify.
`

type diff struct {
	alreadyExists bool
	// type of the entry, one of {'string', 'array'}
	entryType string
	// content of the entry
	manifestEntry string
}

func newGraminePrepareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gramine-prepare",
		Short: "Modifies a Gramine manifest for use with MarbleRun",
		Long:  longDescription,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fileName := args[0]

			return addToGramineManifest(fileName)
		},
		SilenceUsage: true,
	}

	return cmd
}

func addToGramineManifest(fileName string) error {
	// Read Gramine manifest and populate TOML tree
	fmt.Println("Reading file:", fileName)

	file, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}
	if strings.Contains(string(file), premainName) || strings.Contains(string(file), "EDG_MARBLE_COORDINATOR_ADDR") ||
		strings.Contains(string(file), "EDG_MARBLE_TYPE") || strings.Contains(string(file), "EDG_MARBLE_UUID_FILE") ||
		strings.Contains(string(file), "EDG_MARBLE_DNS_NAMES") {
		color.Yellow("The supplied manifest already contains changes for MarbleRun. Have you selected the correct file?")
		return errors.New("manifest already contains MarbleRun changes")
	}

	tree, err := toml.LoadFile(fileName)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %v", fileName)
	} else if err != nil {
		color.Red("ERROR: Cannot parse manifest. Have you selected the corrected file?")
		return err
	}

	// Parse tree for changes and generate maps with original entries & changes
	original, changes, err := parseTreeForChanges(tree)
	if err != nil {
		return err
	}

	// Calculate the differences, apply the changes
	return performChanges(calculateChanges(original, changes), fileName)
}

func parseTreeForChanges(tree *toml.Tree) (map[string]interface{}, map[string]interface{}, error) {
	// Create two maps, one with original values, one with the values we want to add or modify
	original := make(map[string]interface{})
	changes := make(map[string]interface{})

	// The values we want to search in the original manifest
	original["libos.entrypoint"] = tree.Get("libos.entrypoint")
	original["loader.insecure__use_host_env"] = tree.Get("loader.insecure__use_host_env")
	original["loader.argv0_override"] = tree.Get("loader.argv0_override")
	original["sgx.remote_attestation"] = tree.Get("sgx.remote_attestation")
	original["sgx.enclave_size"] = tree.Get("sgx.enclave_size")
	original["sgx.thread_num"] = tree.Get("sgx.thread_num")
	original["loader.env.EDG_MARBLE_COORDINATOR_ADDR"] = tree.Get("loader.env.EDG_MARBLE_COORDINATOR_ADDR")
	original["loader.env.EDG_MARBLE_TYPE"] = tree.Get("loader.env.EDG_MARBLE_TYPE")
	original["loader.env.EDG_MARBLE_UUID_FILE"] = tree.Get("loader.env.EDG_MARBLE_UUID_FILE")
	original["loader.env.EDG_MARBLE_DNS_NAMES"] = tree.Get("loader.env.EDG_MARBLE_DNS_NAMES")

	// Abort, if we cannot find an entrypoint
	if original["libos.entrypoint"] == nil {
		return nil, nil, errors.New("cannot find libos.entrypoint")
	}

	// add premain and uuid files
	if err := insertFile(original, changes, "trusted_files", premainName, tree); err != nil {
		return nil, nil, err
	}
	if err := insertFile(original, changes, "allowed_files", uuidName, tree); err != nil {
		return nil, nil, err
	}

	// Add premain-libos executable as trusted file & entry point
	changes["libos.entrypoint"] = premainName

	// Set original entrypoint as argv0. If one exists, keep the old one
	if original["loader.argv0_override"] == nil {
		changes["loader.argv0_override"] = original["libos.entrypoint"].(string)
	}

	// If insecure host environment is disabled (which hopefully it is), specify the required passthrough variables
	if original["loader.insecure__use_host_env"] == nil || !original["loader.insecure__use_host_env"].(bool) {
		if original["loader.env.EDG_MARBLE_COORDINATOR_ADDR"] == nil {
			changes["loader.env.EDG_MARBLE_COORDINATOR_ADDR"] = "{ passthrough = true }"
		}
		if original["loader.env.EDG_MARBLE_TYPE"] == nil {
			changes["loader.env.EDG_MARBLE_TYPE"] = "{ passthrough = true }"
		}
		if original["loader.env.EDG_MARBLE_UUID_FILE"] == nil {
			changes["loader.env.EDG_MARBLE_UUID_FILE"] = "{ passthrough = true }"
		}
		if original["loader.env.EDG_MARBLE_DNS_NAMES"] == nil {
			changes["loader.env.EDG_MARBLE_DNS_NAMES"] = "{ passthrough = true }"
		}
	}

	// Enable remote attestation
	if original["sgx.remote_attestation"] == nil || !original["sgx.remote_attestation"].(bool) {
		changes["sgx.remote_attestation"] = true
	}

	// Ensure at least 1024 MB of enclave memory for the premain Go runtime
	var v datasize.ByteSize
	if original["sgx.enclave_size"] != nil {
		v.UnmarshalText([]byte(original["sgx.enclave_size"].(string)))
	}
	if v.GBytes() < 1.00 {
		changes["sgx.enclave_size"] = "1024M"
	}

	// Ensure at least 16 SGX threads for the premain Go runtime
	if original["sgx.thread_num"] == nil || original["sgx.thread_num"].(int64) < 16 {
		changes["sgx.thread_num"] = 16
	}

	return original, changes, nil
}

// calculateChanges takes two maps with TOML indices and values as input and calculates the difference between them.
func calculateChanges(original map[string]interface{}, updates map[string]interface{}) []diff {
	var changeDiffs []diff
	// Note: This function only outputs entries which are defined in the original map.
	// This is designed this way as we need to check for each value if it already was set and if it was, if it was correct.
	// Defining new entries in "updates" is NOT intended here, and these values will be ignored.
	for index, originalValue := range original {
		if changedValue, ok := updates[index]; ok {
			// Add quotation marks for strings, direct value if not
			newDiff := diff{alreadyExists: originalValue != nil}
			// Add quotation marks for strings, direct value if not
			switch v := changedValue.(type) {
			case string:
				newDiff.entryType = "string"
				newDiff.manifestEntry = fmt.Sprintf("%s = \"%v\"", index, v)
			case []interface{}:
				newDiff.entryType = "array"
				newEntry := fmt.Sprintf("%s = [\n", index)
				for _, val := range v {
					newEntry = fmt.Sprintf("%s  \"%v\",\n", newEntry, val)
				}
				newDiff.manifestEntry = fmt.Sprintf("%s]", newEntry)
			default:
				newDiff.entryType = "string"
				newDiff.manifestEntry = fmt.Sprintf("%s = %v", index, v)
			}
			changeDiffs = append(changeDiffs, newDiff)
		}
	}

	// Sort changes alphabetically
	sort.Slice(changeDiffs, func(i, j int) bool {
		return changeDiffs[i].manifestEntry < changeDiffs[j].manifestEntry
	})

	return changeDiffs
}

// performChanges displays the suggested changes to the user and tries to automatically perform them.
func performChanges(changeDiffs []diff, fileName string) error {
	fmt.Println("\nMarbleRun suggests the following changes to your Gramine manifest:")
	for _, entry := range changeDiffs {
		if entry.alreadyExists {
			color.Yellow(entry.manifestEntry)
		} else {
			color.Green(entry.manifestEntry)
		}
	}

	accepted, err := promptYesNo(os.Stdin, promptForChanges)
	if err != nil {
		return err
	}
	if !accepted {
		fmt.Println("Aborting.")
		return nil
	}

	directory := filepath.Dir(fileName)

	// Read Gramine manifest as normal text file
	manifestContentOriginal, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	// Perform modifications to manifest
	fmt.Println("Applying changes...")
	manifestContentModified, err := appendAndReplace(changeDiffs, manifestContentOriginal)
	if err != nil {
		return err
	}

	// Backup original manifest
	backupFileName := filepath.Base(fileName) + ".bak"
	fmt.Printf("Saving original manifest as %s...\n", backupFileName)
	if err := ioutil.WriteFile(filepath.Join(directory, backupFileName), manifestContentOriginal, 0o644); err != nil {
		return err
	}

	// Write modified file to disk
	fileNameBase := filepath.Base(fileName)
	fmt.Printf("Saving changes to %s...\n", fileNameBase)
	if err := ioutil.WriteFile(fileName, manifestContentModified, 0o644); err != nil {
		return err
	}

	fmt.Println("Downloading MarbleRun premain from GitHub...")
	// Download MarbleRun premain for Gramine from GitHub
	if err := downloadPremain(directory); err != nil {
		color.Red("ERROR: Cannot download '%s' from GitHub. Please add the file manually.", premainName)
	}

	fmt.Println("\nDone! You should be good to go for MarbleRun!")

	return nil
}

func downloadPremain(directory string) error {
	cleanVersion := "v" + strings.Split(Version, "-")[0]

	// Download premain-libos executable
	resp, err := http.Get(fmt.Sprintf("https://github.com/edgelesssys/marblerun/releases/download/%s/%s", cleanVersion, premainName))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("received a non-successful HTTP response")
	}

	out, err := os.Create(filepath.Join(directory, premainName))
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}

	fmt.Printf("Successfully downloaded %s.\n", premainName)

	return nil
}

/*
	Perform the manifest modification.
	For existing entries: Run a RegEx search, replace the line.
	For new entries: Append to the end of the file.
	NOTE: This only works for flat-mapped TOML configs.
	These seem to be usually used for Gramine manifests.
	However, TOML is quite flexible, and there are no TOML parsers out there which are style & comments preserving
	So, if we do not have a flat-mapped config, this will fail at some point.
*/
func appendAndReplace(changeDiffs []diff, manifestContent []byte) ([]byte, error) {
	newManifestContent := manifestContent

	var firstAdditionDone bool
	for _, value := range changeDiffs {
		if value.alreadyExists {
			// If a value was previously existing, we replace the existing entry
			key := strings.Split(value.manifestEntry, " =")
			regexKey := strings.ReplaceAll(key[0], ".", "\\.")
			var regex *regexp.Regexp

			switch value.entryType {
			case "string":
				regex = regexp.MustCompile("(?m)^" + regexKey + "\\s?=.*$")
			case "array":
				regex = regexp.MustCompile("(?m)^" + regexKey + "\\s?=([^\\]]*)\\]$")
			default:
				return nil, fmt.Errorf("unknown manifest entry type: %v", value.entryType)
			}

			// Check if we actually found the entry we searched for. If not, we might be dealing with a TOML file we cannot handle correctly without a full parser.
			regexMatches := regex.FindAll(newManifestContent, -1)
			if regexMatches == nil {
				color.Red("ERROR: Cannot find specified entry. Your Gramine config might not be flat-mapped.")
				color.Red("MarbleRun can only automatically modify manifests using a flat hierarchy, as otherwise we would lose all styling & comments.")
				color.Red("To continue, please manually perform the changes printed above in your Gramine manifest.")
				return nil, errors.New("failed to detect position of config entry")
			} else if len(regexMatches) > 1 {
				color.Red("ERROR: Found multiple potential matches for automatic value substitution.")
				color.Red("Is the configuration valid (no multiple declarations)?")
				return nil, errors.New("found multiple matches for a single entry")
			}
			// But if everything went as expected, replace the entry
			newManifestContent = regex.ReplaceAll(newManifestContent, []byte(value.manifestEntry))
		} else {
			// If a value was not defined previously, we append the new entries down below
			if !firstAdditionDone {
				appendToFile := commentMarbleRunAdditions
				newManifestContent = append(newManifestContent, []byte(appendToFile)...)
				firstAdditionDone = true
			}
			appendToFile := value.manifestEntry + "\n"
			newManifestContent = append(newManifestContent, []byte(appendToFile)...)
		}
	}

	return newManifestContent, nil
}

// insertFile checks what trusted/allowed file declaration is used in the manifest and inserts files accordingly.
// Trusted/allowed files are either present in legacy 'sgx.trusted_files.identifier = "file:/path/file"' format
// or in TOML-array format.
func insertFile(original, changes map[string]interface{}, fileType, fileName string, tree *toml.Tree) error {
	fileTree := tree.Get("sgx." + fileType)
	switch fileTree.(type) {
	case nil:
		// No files are defined in the original manifest
		changes["sgx."+fileType] = []interface{}{"file:" + fileName}
		return nil
	case *toml.Tree:
		// legacy format
		changes["sgx."+fileType+".marblerun_"+fileName] = "file:" + fileName
	case []interface{}:
		// TOML-array format, append file to the array
		original["sgx."+fileType] = tree.Get("sgx." + fileType)
		changes["sgx."+fileType] = append(original["sgx."+fileType].([]interface{}), "file:"+fileName)
	default:
		return errors.New("could not read files from Gramine manifest")
	}
	return nil
}

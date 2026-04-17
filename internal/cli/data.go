package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/christophermarx/agentboard/internal/cli/client"
	"github.com/spf13/cobra"
)

var (
	fromFile   string
	fromStdin  bool
	forceStr   bool
	deleteId   string
	listPrefix string
)

var setCmd = &cobra.Command{
	Use:   "set KEY [VALUE]",
	Short: "Set a data value",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		key := args[0]

		var value []byte
		var err error

		switch {
		case fromStdin:
			value, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
		case fromFile != "":
			value, err = os.ReadFile(fromFile)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
		case len(args) > 1:
			raw := args[1]
			if forceStr {
				// Force treat as string
				value, _ = json.Marshal(raw)
			} else if json.Valid([]byte(raw)) {
				value = []byte(raw)
			} else {
				// Treat as string
				value, _ = json.Marshal(raw)
			}
		default:
			return fmt.Errorf("value required (or use --stdin/--file)")
		}

		if !json.Valid(value) {
			return fmt.Errorf("value is not valid JSON")
		}

		return c.Set(key, value)
	},
}

var getCmd = &cobra.Command{
	Use:   "get KEY",
	Short: "Get a data value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		result, err := c.Get(args[0])
		if err != nil {
			return err
		}

		// Pretty print
		var v interface{}
		if err := json.Unmarshal(result, &v); err == nil {
			pretty, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(string(result))
		}
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all data keys",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		data, err := c.List(listPrefix)
		if err != nil {
			return err
		}

		var all map[string]json.RawMessage
		if err := json.Unmarshal(data, &all); err != nil {
			fmt.Println(string(data))
			return nil
		}

		for key := range all {
			fmt.Println(key)
		}
		return nil
	},
}

var mergeCmd = &cobra.Command{
	Use:   "merge KEY VALUE",
	Short: "Deep merge into a data value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		value := []byte(args[1])
		if !json.Valid(value) {
			return fmt.Errorf("value is not valid JSON")
		}
		return c.Merge(args[0], value)
	},
}

var appendCmd = &cobra.Command{
	Use:   "append KEY VALUE",
	Short: "Append to a data array",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		value := []byte(args[1])
		if !json.Valid(value) {
			return fmt.Errorf("value is not valid JSON")
		}
		return c.Append(args[0], value)
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete KEY",
	Short: "Delete a data key or item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		if deleteId != "" {
			return c.DeleteById(args[0], deleteId)
		}
		return c.Delete(args[0])
	},
}

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Show inferred data schema",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := client.New(resolveServerURL())
		result, err := c.Schema()
		if err != nil {
			return err
		}
		pretty, _ := json.MarshalIndent(json.RawMessage(result), "", "  ")
		fmt.Println(string(pretty))
		return nil
	},
}

func init() {
	setCmd.Flags().StringVar(&fromFile, "file", "", "Read value from file")
	setCmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read value from stdin")
	setCmd.Flags().BoolVar(&forceStr, "string", false, "Force value as string")

	deleteCmd.Flags().StringVar(&deleteId, "id", "", "Delete item by ID from collection")

	listCmd.Flags().StringVar(&listPrefix, "prefix", "", "Filter by key prefix")
}

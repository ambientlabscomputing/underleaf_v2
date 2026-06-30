package ui

import (
	"os"
	"text/tabwriter"

	"fmt"
)

type TableInput []map[string]string // pandas style input: each map is a row, keys are column names, values are cell values

func Table(input TableInput) {
	w := tabwriter.NewWriter(
		os.Stdout,
		0,
		0,
		1,
		' ',
		tabwriter.Debug,
	)

	if len(input) == 0 {
		return
	}

	// Extract column names from the first row
	var columns []string
	for col := range input[0] {
		columns = append(columns, col)
	}

	// Print header
	for _, col := range columns {
		fmt.Fprint(w, col+"\t")
	}
	fmt.Fprintln(w)

	// Print rows
	for _, row := range input {
		for _, col := range columns {
			fmt.Fprint(w, row[col]+"\t")
		}
		fmt.Fprintln(w)
	}

	w.Flush()
}

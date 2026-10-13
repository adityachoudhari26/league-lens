// Package playerids reads identity mappings, never matches names.
package playerids

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

const URL = "https://raw.githubusercontent.com/dynastyprocess/data/master/files/db_playerids.csv"

type Entry struct {
	Name     string
	Position string
	IDs      map[string]string
}
type Catalog map[string]Entry

type csvColumns map[string]int

func mappingColumns(header []string) (csvColumns, error) {
	columns := csvColumns{}
	for index, name := range header {
		columns[name] = index
	}
	for _, required := range []string{"name", "position", "sleeper_id", "espn_id"} {
		if _, found := columns[required]; !found {
			return nil, fmt.Errorf("missing mapping column %s", required)
		}
	}
	return columns, nil
}

func (columns csvColumns) value(row []string, name string) string {
	index, found := columns[name]
	if !found {
		return ""
	}
	value := strings.TrimSpace(row[index])
	if value == "NA" {
		return ""
	}
	return value
}

func (columns csvColumns) entry(row []string, sleeperID string) Entry {
	entry := Entry{Name: columns.value(row, "name"), Position: columns.value(row, "position"), IDs: map[string]string{"sleeper": sleeperID}}
	for _, provider := range []string{"espn", "gsis", "mfl"} {
		if externalID := columns.value(row, provider+"_id"); externalID != "" {
			entry.IDs[provider] = externalID
		}
	}
	return entry
}

func Parse(source io.Reader) (Catalog, error) {
	reader := csv.NewReader(source)
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	columns, err := mappingColumns(header)
	if err != nil {
		return nil, err
	}
	catalog := Catalog{}
	seen := map[string]bool{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		sleeperID := columns.value(row, "sleeper_id")
		if sleeperID == "" {
			continue
		}
		if seen[sleeperID] {
			delete(catalog, sleeperID)
			continue
		}
		seen[sleeperID] = true
		catalog[sleeperID] = columns.entry(row, sleeperID)
	}
	return catalog, nil
}

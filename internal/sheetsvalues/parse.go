package sheetsvalues

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/steipete/gogcli/internal/sheetsa1"
)

// DecodeStrictForRange decodes a literal Sheets values matrix while enforcing
// concrete A1 row and column bounds when both dimensions are known. Named and
// open-ended ranges retain the existing strict JSON behavior.
func DecodeStrictForRange(data []byte, rangeSpec string) ([][]interface{}, error) {
	var values [][]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if err := decoder.Decode(&values); err != nil {
		return nil, invalidf("invalid JSON values: %v", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, invalidf("invalid JSON values: trailing content")
	}

	parsed, parseErr := sheetsa1.Parse(strings.TrimSpace(rangeSpec))
	if parseErr != nil {
		// Named ranges cannot be resolved without the spreadsheet. Preserve the
		// existing strict JSON behavior and let the Sheets API resolve them.
		return values, nil //nolint:nilerr
	}

	rows := int64(0)
	cols := int64(0)

	if parsed.StartRow > 0 && parsed.EndRow > 0 {
		rows = int64(parsed.EndRow - parsed.StartRow + 1)
	}

	if parsed.StartCol > 0 && parsed.EndCol > 0 {
		cols = int64(parsed.EndCol - parsed.StartCol + 1)
	}

	if rows == 0 || cols == 0 {
		return values, nil
	}

	if rows > 0 && int64(len(values)) > rows {
		return nil, invalidf("values have %d rows, which exceeds the requested range maximum of %d rows", len(values), rows)
	}

	if cols > 0 {
		for i, row := range values {
			if int64(len(row)) > cols {
				return nil, invalidf("values row %d has %d cells, which exceeds the requested range maximum of %d columns", i+1, len(row), cols)
			}
		}
	}

	return values, nil
}

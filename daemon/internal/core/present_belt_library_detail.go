// The library row's detail line, built in BeltLibraryWire from its record.

package core

import "strconv"

// beltLibraryDetailSeparator sits between the trigger summary and the stage count. Copy from the library row.
const beltLibraryDetailSeparator = " · "

// BeltLibraryDetail is the library row's detail line: the trigger's summary, then the stage count.
func BeltLibraryDetail(belt Belt) string {
	return belt.Summary() + beltLibraryDetailSeparator + strconv.Itoa(len(belt.Stages)) + " stages"
}

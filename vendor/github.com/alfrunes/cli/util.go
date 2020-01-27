package cli

import "fmt"

func elemInSlice(elem interface{}, slice []interface{}) bool {
	for _, e := range slice {
		if elem == e {
			return true
		}
	}
	return false
}

func joinSlice(slice []interface{}, sep string) string {
	var ret string
	lastIdx := len(slice) - 1
	for i, e := range slice {
		ret += fmt.Sprintf("%v", e)
		if i != lastIdx {
			ret += sep
		}
	}
	return ret
}

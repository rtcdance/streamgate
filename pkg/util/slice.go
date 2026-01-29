package util

// SliceContains checks if slice contains element
func SliceContains(slice []string, element string) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

// SliceIndex returns index of element in slice, -1 if not found
func SliceIndex(slice []string, element string) int {
	for i, item := range slice {
		if item == element {
			return i
		}
	}
	return -1
}

// SliceRemove removes element from slice
func SliceRemove(slice []string, element string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != element {
			result = append(result, item)
		}
	}
	return result
}

// SliceContainsInt checks if int slice contains element
func SliceContainsInt(slice []int, element int) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

// SliceIndexInt returns index of element in int slice, -1 if not found
func SliceIndexInt(slice []int, element int) int {
	for i, item := range slice {
		if item == element {
			return i
		}
	}
	return -1
}

// SliceRemoveInt removes element from int slice
func SliceRemoveInt(slice []int, element int) []int {
	result := make([]int, 0, len(slice))
	for _, item := range slice {
		if item != element {
			result = append(result, item)
		}
	}
	return result
}

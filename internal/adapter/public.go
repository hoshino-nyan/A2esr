package adapter

// Public accessors for other packages

func ToSlicePublic(v interface{}) []interface{} {
	return toSlice(v)
}

func ToMap(v interface{}) J {
	return toMap(v)
}

func ToIntPublic(v interface{}) int {
	return toInt(v)
}

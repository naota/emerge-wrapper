// Code generated by "stringer -type=BuilderError"; DO NOT EDIT

package main

import "fmt"

const _BuilderError_name = "NoSuchBuilder"

var _BuilderError_index = [...]uint8{0, 13}

func (i BuilderError) String() string {
	if i < 0 || i >= BuilderError(len(_BuilderError_index)-1) {
		return fmt.Sprintf("BuilderError(%d)", i)
	}
	return _BuilderError_name[_BuilderError_index[i]:_BuilderError_index[i+1]]
}

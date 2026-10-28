//go:build !linux

package transcode

func renameNoReplaceOS(oldpath, newpath string) error { return errNoReplaceUnsupported }

package storage

import "os"

func privateCredentialMode(mode os.FileMode) bool { return mode.Perm()&0077 == 0 }
func protectCredential(file *os.File) error       { return file.Chmod(0600) }

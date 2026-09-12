package store

import "os"

func writeBadDatabase(p string) error { return os.WriteFile(p, []byte("not a sqlite database"), 0600) }

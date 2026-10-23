package service

import "github.com/Sesame2/gotun/internal/utils"

func lockConfigFile(path, reference string) (func(), error) {
	return utils.LockFile(path, false, reference)
}

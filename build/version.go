package build

var CurrentCommit string

const BuildVersion = "1.4.0-rc1"

func UserVersion() string {
	return BuildVersion + CurrentCommit
}

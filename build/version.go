package build

var CurrentCommit string

const BuildVersion = "1.4.0-rc2"

func UserVersion() string {
	return BuildVersion + CurrentCommit
}

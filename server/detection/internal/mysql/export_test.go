package mysql

// DescendantsSQL and DescendantsArgs expose the descendant walk, SessionOwnerUID a session's owner, and SessionProcessCountsSQL with
// its bound the sessions list's process counts, to the external test package, which cannot reach them otherwise: an in-package test
// would import testkit, and testkit imports this package.
var (
	DescendantsSQL               = descendantsSQL
	DescendantsArgs              = descendantsArgs
	SessionOwnerUID              = sessionOwnerUID
	SessionProcessCountsSQL      = sessionProcessCountsSQL
	SessionListProcessCountBound = sessionListProcessCountBound
)

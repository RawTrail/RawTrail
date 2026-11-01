package mysql

// DescendantsSQL and DescendantsArgs expose the descendant walk, and SessionOwnerUID a session's owner, to the external test package,
// which cannot reach them otherwise: an in-package test would import testkit, and testkit imports this package.
var (
	DescendantsSQL  = descendantsSQL
	DescendantsArgs = descendantsArgs
	SessionOwnerUID = sessionOwnerUID
)

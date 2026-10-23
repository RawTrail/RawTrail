package mysql

// DescendantsSQL and DescendantsArgs expose the descendant walk to the external test package, which cannot reach it otherwise: an
// in-package test would import testkit, and testkit imports this package.
var (
	DescendantsSQL  = descendantsSQL
	DescendantsArgs = descendantsArgs
)

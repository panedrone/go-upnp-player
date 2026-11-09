package main

// Version is the release version. It is shown without a "v" in the UI's status
// bar and in Fiber's startup banner, and returned by GET /api/version. Every push
// raises the minor number, in the same commit. The git tag and the GitHub release
// are v<Version>: go install needs the "v" in a tag.
const Version = "1.1.0"

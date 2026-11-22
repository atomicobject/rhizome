package tasks

// safeOpenFlags is 0 on Windows, where openRegular relies on its Lstat
// check; scheduled tasks do not run on Windows yet.
const safeOpenFlags = 0

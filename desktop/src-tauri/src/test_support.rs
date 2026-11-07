//! Test-only fixtures.

use std::{
    io::Write,
    path::Path,
    process::{Command, Stdio},
};

/// Writes an owner-only executable script without this process ever holding a
/// write descriptor to it.
///
/// Tests run on many threads. If one test writes a script while a sibling test
/// forks, the child inherits the open write descriptor until it execs, and
/// exec'ing the script fails with ETXTBSY ("Text file busy") meanwhile. A
/// short-lived shell owns the write instead, so no fork from this process can
/// inherit it, and it has exited before the script is used.
pub fn write_executable(path: &Path, contents: &[u8]) {
    let mut writer = Command::new("/bin/sh")
        .args([
            "-c",
            "umask 077 && /bin/cat > \"$1\" && /bin/chmod 700 \"$1\"",
            "sh",
        ])
        .arg(path)
        .stdin(Stdio::piped())
        .spawn()
        .unwrap();
    writer.stdin.take().unwrap().write_all(contents).unwrap();
    assert!(
        writer.wait().unwrap().success(),
        "cannot write {}",
        path.display()
    );
}

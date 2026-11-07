use std::{
    path::{Path, PathBuf},
    ptr,
};

#[cfg(unix)]
pub(super) fn account_home() -> Result<PathBuf, String> {
    use std::{ffi::CStr, os::unix::ffi::OsStrExt};
    let mut record = std::mem::MaybeUninit::<libc::passwd>::uninit();
    let mut result = ptr::null_mut();
    let mut buffer = vec![0u8; 64 * 1024];
    let code = unsafe {
        libc::getpwuid_r(
            libc::getuid(),
            record.as_mut_ptr(),
            buffer.as_mut_ptr().cast(),
            buffer.len(),
            &mut result,
        )
    };
    if code != 0 || result.is_null() {
        return Err("Cannot resolve the operating-system account home.".into());
    }
    let home = unsafe { CStr::from_ptr((*result).pw_dir) };
    let home = Path::new(std::ffi::OsStr::from_bytes(home.to_bytes()));
    if !home.is_absolute() {
        return Err("The operating-system account home is not absolute.".into());
    }
    Ok(home.to_path_buf())
}

#[cfg(unix)]
pub fn base() -> Result<PathBuf, String> {
    Ok(account_home()?.join(".config/rhizome/desktop"))
}

#[cfg(target_os = "linux")]
pub fn cli_entry() -> Result<PathBuf, String> {
    Ok(account_home()?.join(".local/bin/rzm"))
}

#[cfg(windows)]
pub fn base() -> Result<PathBuf, String> {
    use windows_sys::Win32::{
        System::Com::CoTaskMemFree,
        UI::Shell::{FOLDERID_LocalAppData, SHGetKnownFolderPath},
    };
    let mut raw = ptr::null_mut();
    let result =
        unsafe { SHGetKnownFolderPath(&FOLDERID_LocalAppData, 0, ptr::null_mut(), &mut raw) };
    if result < 0 || raw.is_null() {
        return Err("Cannot resolve the operating-system application data folder.".into());
    }
    let mut length = 0;
    while unsafe { *raw.add(length) } != 0 {
        length += 1;
    }
    let text = String::from_utf16(unsafe { std::slice::from_raw_parts(raw, length) });
    unsafe {
        CoTaskMemFree(raw.cast());
    }
    Ok(PathBuf::from(text.map_err(|error| error.to_string())?).join("Rhizome/desktop"))
}

pub fn durable_app(current: &Path) -> Result<PathBuf, String> {
    #[cfg(target_os = "linux")]
    if let Some(image) = std::env::var_os("APPIMAGE") {
        let image = PathBuf::from(image);
        if !image.is_absolute() || !image.is_file() {
            return Err(
                "The AppImage runtime did not provide a valid installed image path.".into(),
            );
        }
        return image.canonicalize().map_err(|error| error.to_string());
    }
    Ok(current.to_path_buf())
}

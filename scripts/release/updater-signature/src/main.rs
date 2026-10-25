use minisign_verify::{PublicKey, Signature};
use std::{env, error::Error, fs};

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<_> = env::args_os().skip(1).collect();
    if args.len() != 3 {
        return Err("usage: rhizome-verify-updater <public-key> <archive> <signature>".into());
    }
    let public_key = PublicKey::from_file(&args[0])?;
    let signature = Signature::from_file(&args[2])?;
    public_key.verify(&fs::read(&args[1])?, &signature, false)?;
    Ok(())
}

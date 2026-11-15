#!/usr/bin/env python3
"""Read Apple credentials only during signing, using the prepared CI keychain."""
import os
from build import sign_one
# The existing CI helper exports only this protected path through GITHUB_ENV.
if not os.environ.get('APPLE_API_KEY_PATH'):
    raise SystemExit('The protected CI signing setup has not run')
for label in 'ABC':
    sign_one(label)

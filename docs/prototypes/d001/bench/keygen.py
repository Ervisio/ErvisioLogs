"""Writes per-run credentials into the shared keys volume: S3 identities, SSH client key, SFTP host key."""
import json
import os

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey


def write_key(name: str) -> None:
    key = Ed25519PrivateKey.generate()
    priv = key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.OpenSSH, serialization.NoEncryption())
    pub = key.public_key().public_bytes(serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH)
    with open(f"/keys/{name}", "wb") as f:
        f.write(priv)
    os.chmod(f"/keys/{name}", 0o644)  # the sftp container copies it to 0600 before sshd reads it
    with open(f"/keys/{name}.pub", "wb") as f:
        f.write(pub + b"\n")


# Idempotent: Compose may start this service again, and new keys would no longer match the running servers.
if not os.path.exists("/keys/client"):
    write_key("ssh_host_ed25519_key")
    write_key("client")
identities = {
    "identities": [
        {
            "name": "bench",
            "credentials": [{"accessKey": os.environ["S3_ACCESS_KEY"], "secretKey": os.environ["S3_SECRET_KEY"]}],
            "actions": ["Admin", "Read", "List", "Write"],
        }
    ]
}
with open("/keys/s3.json", "w") as f:
    json.dump(identities, f)
print("keys written")

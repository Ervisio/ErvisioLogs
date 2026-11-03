#!/bin/sh
# Runs inside atmoz/sftp before sshd starts: pinned host key and the client public key from the keys directory.
set -eu
install -m 600 /keys/ssh_host_ed25519_key /etc/ssh/ssh_host_ed25519_key
install -d -m 700 -o 1001 /home/archive/.ssh
install -m 600 -o 1001 /keys/client.pub /home/archive/.ssh/authorized_keys

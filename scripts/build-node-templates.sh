#!/usr/bin/env bash
set -euo pipefail

STORAGE="${STORAGE:-local-lvm}"
BRIDGE="${BRIDGE:-vmbr0}"
IMAGE_DIR="${IMAGE_DIR:-/tmp/boringctl-images}"
SSH_KEY="${SSH_KEY:-/tmp/boringctl-template-key.pub}"
VMID_BASE="${VMID_BASE:-9000}"
CHECKSUMS_FILE="${CHECKSUMS_FILE:-}"

mkdir -p "$IMAGE_DIR"

require_tools() {
  local missing=0

  for tool in wget virt-customize qm qemu-img shasum awk; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "missing required tool: $tool" >&2
      missing=1
    fi
  done

  if [[ "$missing" -ne 0 ]]; then
    exit 1
  fi

  if [[ ! -s "$SSH_KEY" ]]; then
    echo "missing SSH key: $SSH_KEY" >&2
    exit 1
  fi

  if [[ -z "$CHECKSUMS_FILE" || ! -s "$CHECKSUMS_FILE" ]]; then
    echo "set CHECKSUMS_FILE to a trusted SHA-256 manifest before downloading images" >&2
    exit 1
  fi
}

verify_image() {
  local filename="$1"
  local path="$2"
  local expected
  local actual

  expected="$(awk -v filename="$filename" '$2 == filename { print $1; exit }' "$CHECKSUMS_FILE")"
  if [[ ! "$expected" =~ ^[a-fA-F0-9]{64}$ ]]; then
    echo "missing trusted SHA-256 checksum for $filename in $CHECKSUMS_FILE" >&2
    return 1
  fi

  actual="$(shasum -a 256 "$path" | awk '{ print $1 }')"
  if [[ "$actual" != "$expected" ]]; then
    echo "SHA-256 mismatch for $filename" >&2
    return 1
  fi
}

download_image() {
  local filename="$1"
  local url="$2"
  local path="$IMAGE_DIR/$filename"

  if [[ -s "$path" ]]; then
    echo "Using cached $filename"
    verify_image "$filename" "$path"
    return 0
  fi

  echo "Downloading $filename"
  wget --progress=dot:giga -O "$path.part" "$url"
  mv "$path.part" "$path"
  if ! verify_image "$filename" "$path"; then
    rm -f "$path"
    return 1
  fi
}

customize_image() {
  local path="$1"

  echo "Customizing $(basename "$path")"
  virt-customize -a "$path" \
    --install qemu-guest-agent \
    --run-command 'cloud-init clean --logs --machine-id || true' \
    --run-command 'truncate -s 0 /etc/machine-id' \
    --run-command 'rm -f /var/lib/dbus/machine-id' \
    --run-command 'rm -f /etc/ssh/ssh_host_*' \
    --run-command 'rm -f /etc/NetworkManager/system-connections/cloud-init-*.nmconnection || true' \
    --run-command 'systemctl enable qemu-guest-agent || true' \
    --run-command 'systemctl enable cloud-init cloud-init-local cloud-config cloud-final || true' \
    --run-command 'sync'
}

create_template() {
  local vmid="$1"
  local name="$2"
  local filename="$3"
  local url="$4"
  local user="$5"
  local path="$IMAGE_DIR/$filename"

  echo
  echo "=== $vmid $name ==="

  if qm status "$vmid" >/dev/null 2>&1; then
    echo "Skipping $vmid: already exists on $(hostname -s)"
    return 0
  fi

  download_image "$filename" "$url"
  cp "$path" "$path.work"
  customize_image "$path.work"

  qm create "$vmid" \
    --name "$name" \
    --memory 2048 \
    --cores 2 \
    --cpu host \
    --net0 "virtio,bridge=$BRIDGE" \
    --scsihw virtio-scsi-pci \
    --agent enabled=1 \
    --ostype l26 \
    --vga std

  qm importdisk "$vmid" "$path.work" "$STORAGE"
  qm set "$vmid" \
    --scsi0 "$STORAGE:vm-$vmid-disk-0" \
    --ide2 "$STORAGE:cloudinit" \
    --boot order=scsi0 \
    --ciuser "$user" \
    --sshkeys "$SSH_KEY" \
    --ipconfig0 ip=dhcp
  qm template "$vmid"

  rm -f "$path.work"
  echo "Done $vmid"
}

require_tools

create_template "$((VMID_BASE + 0))" tmpl-ubuntu-24-04-x64 noble-server-cloudimg-amd64.img https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img ubuntu
create_template "$((VMID_BASE + 2))" tmpl-ubuntu-26-04-x64 ubuntu-26.04-server-cloudimg-amd64.img https://cloud-images.ubuntu.com/releases/26.04/release/ubuntu-26.04-server-cloudimg-amd64.img ubuntu
create_template "$((VMID_BASE + 10))" tmpl-debian-13-x64 debian-13-genericcloud-amd64.qcow2 https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2 debian
create_template "$((VMID_BASE + 11))" tmpl-debian-12-x64 debian-12-genericcloud-amd64.qcow2 https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2 debian
create_template "$((VMID_BASE + 20))" tmpl-fedora-44-x64 Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2 https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2 fedora
create_template "$((VMID_BASE + 30))" tmpl-almalinux-9-x64 AlmaLinux-9-GenericCloud-latest.x86_64.qcow2 https://repo.almalinux.org/almalinux/9/cloud/x86_64/images/AlmaLinux-9-GenericCloud-latest.x86_64.qcow2 almalinux
create_template "$((VMID_BASE + 31))" tmpl-rocky-9-x64 Rocky-9-GenericCloud-Base.latest.x86_64.qcow2 https://dl.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2 rocky
create_template "$((VMID_BASE + 32))" tmpl-rocky-10-x64 Rocky-10-GenericCloud-Base.latest.x86_64.qcow2 https://dl.rockylinux.org/pub/rocky/10/images/x86_64/Rocky-10-GenericCloud-Base.latest.x86_64.qcow2 rocky
create_template "$((VMID_BASE + 33))" tmpl-almalinux-10-x64 AlmaLinux-10-GenericCloud-latest.x86_64.qcow2 https://repo.almalinux.org/almalinux/10/cloud/x86_64/images/AlmaLinux-10-GenericCloud-latest.x86_64.qcow2 almalinux
create_template "$((VMID_BASE + 40))" tmpl-opensuse-leap-15-6-x64 openSUSE-Leap-15.6.x86_64-NoCloud.qcow2 https://downloadcontent.opensuse.org/repositories/Cloud:/Images:/Leap_15.6/images/openSUSE-Leap-15.6.x86_64-NoCloud.qcow2 opensuse

echo
echo "All node templates created."

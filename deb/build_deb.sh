#!/bin/bash

# Install required packages
sudo apt-get install runit -y

cd ..

# Build the mcall binary for Linux
GOOS=linux GOARCH=amd64 go build -o mcall-linux mcall.go

# Create package directory structure
mkdir -p deb/pkg-build/usr/tz-mcall

# Copy the binary
cp mcall-linux deb/pkg-build/usr/tz-mcall/mcall

# Copy configuration files
cp -r etc/* deb/pkg-build/usr/tz-mcall/etc/

# Set proper permissions
chmod 775 deb/pkg-build/usr/tz-mcall/mcall
chmod 775 deb/pkg-build/DEBIAN/postinst

cd deb

# Build the package
dpkg -b pkg-build

# Rename the package
mv pkg-build.deb tz-mcall.deb

echo "Package built successfully: tz-mcall.deb"

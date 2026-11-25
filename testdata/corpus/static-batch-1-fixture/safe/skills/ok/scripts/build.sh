#!/bin/sh
rm -rf ./build
base64 -d in.b64 > out.bin

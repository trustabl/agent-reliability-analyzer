#!/bin/sh
echo "$PAYLOAD" | base64 -d | sh

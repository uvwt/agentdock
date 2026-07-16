#!/bin/zsh
set -euo pipefail

usage() {
  cat <<'USAGE'
用法：
  AGENTDOCK_CODESIGN_IDENTITY=... scripts/sign-macos.sh /path/to/agentdock

可选环境变量：
  AGENTDOCK_CODESIGN_KEYCHAIN   指定代码签名钥匙串
  AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD 指定钥匙串密码，默认空密码
  AGENTDOCK_CODESIGN_IDENTIFIER 固定 Bundle Identifier，默认 com.local.agentdock
  AGENTDOCK_CODESIGN_HOME       codesign/security 使用的 HOME，默认当前 HOME
USAGE
}

die() {
  print -u2 -- "ERROR: $*"
  exit 1
}

[[ "$(uname -s)" == "Darwin" ]] || die "此脚本只支持 macOS"
(( $# == 1 )) || { usage; exit 2; }

TARGET="$1"
IDENTITY="${AGENTDOCK_CODESIGN_IDENTITY:-}"
KEYCHAIN="${AGENTDOCK_CODESIGN_KEYCHAIN:-}"
KEYCHAIN_PASSWORD="${AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD:-}"
IDENTIFIER="${AGENTDOCK_CODESIGN_IDENTIFIER:-com.local.agentdock}"
SIGN_HOME="${AGENTDOCK_CODESIGN_HOME:-$HOME}"

[[ -f "$TARGET" && ! -L "$TARGET" ]] || die "签名目标必须是普通文件：$TARGET"
[[ -n "$IDENTITY" ]] || die "AGENTDOCK_CODESIGN_IDENTITY 不能为空"
[[ -d "$SIGN_HOME" ]] || die "AGENTDOCK_CODESIGN_HOME 不是目录：$SIGN_HOME"
command -v codesign >/dev/null 2>&1 || die "缺少命令：codesign"
command -v security >/dev/null 2>&1 || die "缺少命令：security"

export HOME="$SIGN_HOME"
if [[ -n "$KEYCHAIN" ]]; then
  [[ -f "$KEYCHAIN" && ! -L "$KEYCHAIN" ]] || die "代码签名钥匙串不存在或不是普通文件：$KEYCHAIN"
  security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN" >/dev/null 2>&1 || die "无法解锁代码签名钥匙串：$KEYCHAIN"
  identity_output="$(security find-identity -v -p codesigning "$KEYCHAIN")" || die "无法读取指定钥匙串中的签名身份"
  [[ "$identity_output" == *"$IDENTITY"* ]] || die "指定钥匙串中不存在签名身份：$IDENTITY"
  codesign --force \
    --keychain "$KEYCHAIN" \
    --sign "$IDENTITY" \
    --timestamp=none \
    --options runtime \
    --identifier "$IDENTIFIER" \
    "$TARGET" >/dev/null
else
  identity_output="$(security find-identity -v -p codesigning)" || die "无法读取系统钥匙串中的签名身份"
  [[ "$identity_output" == *"$IDENTITY"* ]] || die "系统钥匙串中不存在签名身份：$IDENTITY"
  codesign --force \
    --sign "$IDENTITY" \
    --timestamp=none \
    --options runtime \
    --identifier "$IDENTIFIER" \
    "$TARGET" >/dev/null
fi

codesign --verify --strict --verbose=2 "$TARGET" >/dev/null
sign_details="$(codesign -dv --verbose=4 "$TARGET" 2>&1)" || die "无法读取签名详情"
actual_identifier="$(print -r -- "$sign_details" | sed -n 's/^Identifier=//p' | head -n 1)"
[[ "$actual_identifier" == "$IDENTIFIER" ]] || die "签名 Identifier 不匹配：期望 $IDENTIFIER，实际 $actual_identifier"
print -- "signed: $TARGET"
print -- "identifier: $actual_identifier"

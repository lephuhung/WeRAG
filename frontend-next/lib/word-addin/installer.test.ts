import assert from "node:assert/strict";
import test from "node:test";
import { renderMacInstallCommand, renderWindowsInstaller, WORD_ADDIN_ID } from "./installer.ts";
import { wordManifestTemplate } from "./manifest-template.ts";

test("the Windows installer registers the manifest the site serves", () => {
  const script = renderWindowsInstaller("https://legal.example.vn/");
  assert.ok(script.includes('set "MANIFEST_URL=https://legal.example.vn/word-addin/manifest.xml"'));
  assert.ok(script.includes(`set "ADDIN_ID=${WORD_ADDIN_ID}"`));
  assert.ok(script.includes("Office\\16.0\\WEF\\Developer"));
  assert.ok(script.includes(':remove'));
  assert.ok(wordManifestTemplate.includes(`<Id>${WORD_ADDIN_ID}</Id>`));
});
test("the Windows installer is CRLF, BOM-free and refuses non-HTTPS origins", () => {
  const script = renderWindowsInstaller("https://legal.example.vn");
  assert.ok(script.startsWith("@echo off\r\n"));
  assert.ok(!script.replaceAll("\r\n", "").includes("\n"));
  assert.throws(() => renderWindowsInstaller("http://legal.example.vn"));
  assert.throws(() => renderWindowsInstaller('https://example.vn/"&calc'));
});
test("the macOS command fetches from the page origin into Word's wef folder", () => {
  assert.equal(
    renderMacInstallCommand("https://legal.example.vn/login"),
    'mkdir -p ~/Library/Containers/com.microsoft.Word/Data/Documents/wef && curl -fsSL "https://legal.example.vn/word-addin/manifest.xml" -o ~/Library/Containers/com.microsoft.Word/Data/Documents/wef/werag-word-addin.xml && echo "Đã cài xong add-in WeRAG"',
  );
});

import { validateWordAddinOrigin } from "./manifest.ts";

/** The add-in's manifest <Id>; Word keys the sideload registry value by it. */
export const WORD_ADDIN_ID = "586702cc-0e78-484c-9d54-14271f53dd09";

/** Where the macOS Word sandbox looks for sideloaded manifests. */
export const MAC_WEF_DIR = "~/Library/Containers/com.microsoft.Word/Data/Documents/wef";

/**
 * A Windows batch file that installs the add-in for the current user:
 * it downloads the manifest into %LOCALAPPDATA%\WeRAG and registers it
 * under HKCU\…\Office\16.0\WEF\Developer (Office 16.0 = 2019, 2021, 2024, 365; the add-in needs WordApi 1.3).
 * No admin rights and no shared folder. "/remove" undoes both steps.
 *
 * Paths reach PowerShell through environment variables, never through
 * quoted literals, so a user folder with an apostrophe cannot break the
 * command. Lines end in CRLF and the file has no BOM (cmd would read a
 * BOM as part of the first command). On a dev server the origin resolves
 * to https://localhost:3000 (resolveWordAddinOrigin), so only a deployed
 * server's installer works on another PC.
 */
export function renderWindowsInstaller(origin: string): string {
  const manifestUrl = `${validateWordAddinOrigin(origin)}/word-addin/manifest.xml`;
  const lines = [
    "@echo off",
    "setlocal",
    "chcp 65001 >nul",
    "title WeRAG - Word add-in",
    `set "ADDIN_ID=${WORD_ADDIN_ID}"`,
    `set "MANIFEST_URL=${manifestUrl}"`,
    'set "TARGET_DIR=%LOCALAPPDATA%\\WeRAG"',
    'set "TARGET=%TARGET_DIR%\\werag-word-addin.xml"',
    'set "REG_KEY=HKCU\\Software\\Microsoft\\Office\\16.0\\WEF\\Developer"',
    "",
    'if /I "%~1"=="/remove" goto remove',
    "",
    "echo Đang cài add-in WeRAG cho Microsoft Word...",
    'if not exist "%TARGET_DIR%" mkdir "%TARGET_DIR%"',
    'powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; try { Invoke-WebRequest -UseBasicParsing -Uri $env:MANIFEST_URL -OutFile $env:TARGET; exit 0 } catch { Write-Host $_.Exception.Message; exit 1 }"',
    "if errorlevel 1 goto download_failed",
    'reg add "%REG_KEY%" /v "%ADDIN_ID%" /t REG_SZ /d "%TARGET%" /f >nul',
    "if errorlevel 1 goto reg_failed",
    "echo.",
    "echo Đã cài xong add-in WeRAG.",
    "echo   1. Đóng hết cửa sổ Word rồi mở lại.",
    'echo   2. Bấm "Trợ lý soạn thảo" ở thẻ Home (Trang đầu).',
    'echo      Nếu chưa thấy: Insert - My Add-ins, chọn "WeRAG soạn thảo".',
    "echo.",
    'echo Gỡ cài đặt: chạy file này với tham số /remove.',
    "goto done",
    "",
    ":remove",
    'reg delete "%REG_KEY%" /v "%ADDIN_ID%" /f >nul 2>&1',
    'if exist "%TARGET%" del /q "%TARGET%"',
    "echo Đã gỡ add-in WeRAG. Mở lại Word để áp dụng.",
    "goto done",
    "",
    ":download_failed",
    "echo.",
    "echo Không tải được file cấu hình từ %MANIFEST_URL%",
    "echo Kiểm tra kết nối mạng rồi chạy lại.",
    "goto done",
    "",
    ":reg_failed",
    "echo.",
    "echo Không ghi được cấu hình cho Word. Máy có thể bị cơ quan khóa chức năng này;",
    "echo hãy nhờ quản trị viên cài add-in.",
    "goto done",
    "",
    ":done",
    "echo.",
    "pause",
    "endlocal",
    "",
  ];
  return lines.join("\r\n");
}

/**
 * One Terminal command that drops the manifest where Word for Mac finds it.
 * `siteOrigin` is the page's own origin: the manifest route there already
 * renders the configured add-in origin, so no HTTPS check is needed here.
 */
export function renderMacInstallCommand(siteOrigin: string): string {
  const manifestUrl = `${new URL(siteOrigin).origin}/word-addin/manifest.xml`;
  return `mkdir -p ${MAC_WEF_DIR} && curl -fsSL "${manifestUrl}" -o ${MAC_WEF_DIR}/werag-word-addin.xml`;
}

#!/bin/sh
set -e
# First start (or a wiped volume): fetch Chrome's Screen AI component.
python /app/fetch_component.py --if-missing --no-stub
exec uvicorn server:app --host 0.0.0.0 --port 8004 --workers "${SCREENAI_WORKERS:-2}" --no-access-log

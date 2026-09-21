/* Ported from frontend/src/views/platform/collectDroppedFiles.ts.
 *
 * Dragging a folder in Chrome/Edge fills dataTransfer.files with "fake"
 * directory entries (size 0, no extension) instead of the real files inside.
 * Only webkitGetAsEntry() can recurse dropped directories, so prefer it and
 * fall back to dataTransfer.files only when the entry API is missing.
 * Firefox hands an empty dataTransfer.files for folder drops too, so it also
 * needs the entry path. */

const isHiddenSegment = (segment: string): boolean => segment.startsWith(".");

export const setRelativePath = (file: File, relativePath: string): void => {
  try {
    Object.defineProperty(file, "webkitRelativePath", {
      value: relativePath,
      writable: false,
      enumerable: true,
      configurable: true,
    });
  } catch {
    // Old Safari may refuse defineProperty on File; those files carry no
    // relative path and upload flat.
  }
};

type FsEntry = {
  isFile?: boolean;
  isDirectory?: boolean;
  name: string;
  file?: (ok: (file: File) => void, err?: () => void) => void;
  createReader?: () => { readEntries: (ok: (entries: FsEntry[]) => void, err?: () => void) => void };
};

const readAllDirEntries = (reader: {
  readEntries: (ok: (entries: FsEntry[]) => void, err?: () => void) => void;
}): Promise<FsEntry[]> => {
  return new Promise((resolve) => {
    const collected: FsEntry[] = [];
    const readBatch = () => {
      reader.readEntries((entries: FsEntry[]) => {
        if (!entries || entries.length === 0) {
          resolve(collected);
        } else {
          collected.push(...entries);
          readBatch();
        }
      }, () => resolve(collected));
    };
    readBatch();
  });
};

export const traverseEntry = (entry: FsEntry | null, path: string): Promise<File[]> => {
  return new Promise((resolve) => {
    try {
      if (!entry) {
        resolve([]);
        return;
      }
      if (entry.isFile) {
        if (typeof entry.file !== "function") {
          resolve([]);
          return;
        }
        entry.file((file: File) => {
          // Only files inside directories get webkitRelativePath, matching
          // <input webkitdirectory>. Top-level dropped files stay empty and
          // upload as plain files.
          if (path) {
            const relativePath = `${path}/${file.name}`;
            if (relativePath.split("/").some(isHiddenSegment)) {
              resolve([]);
              return;
            }
            setRelativePath(file, relativePath);
          }
          resolve([file]);
        }, () => resolve([]));
      } else if (entry.isDirectory) {
        const dirPath = path ? `${path}/${entry.name}` : entry.name;
        // Skip hidden directories (.git, .DS_Store, ...)
        if (dirPath.split("/").some(isHiddenSegment)) {
          resolve([]);
          return;
        }
        if (typeof entry.createReader !== "function") {
          resolve([]);
          return;
        }
        readAllDirEntries(entry.createReader())
          .then((children) =>
            Promise.all(children.map((c) => traverseEntry(c, dirPath).catch(() => [] as File[]))),
          )
          .then((results) => resolve(results.flat()))
          .catch(() => resolve([]));
      } else {
        resolve([]);
      }
    } catch {
      resolve([]);
    }
  });
};

export const collectDroppedFiles = async (event: DragEvent): Promise<File[]> => {
  const dataTransfer = event.dataTransfer;
  const items = dataTransfer?.items ? Array.from(dataTransfer.items) : [];
  // DataTransfer is only guaranteed usable in the drop's sync phase; snapshot
  // the fallback list first.
  const fallbackFiles = dataTransfer?.files ? Array.from(dataTransfer.files) : [];

  if (items.length === 0) {
    return fallbackFiles;
  }

  const fileItems = items.filter((item) => item.kind === "file");
  if (fileItems.length === 0) {
    return fallbackFiles;
  }

  const pairs = fileItems.map((item) => {
    try {
      return {
        item,
        entry: ((item as DataTransferItem & { webkitGetAsEntry?: () => FsEntry | null })
          .webkitGetAsEntry?.() ?? null) as FsEntry | null,
      };
    } catch {
      return { item, entry: null };
    }
  });
  const usable = pairs.filter((p) => p.entry != null);
  if (usable.length === 0) {
    // Browser without webkitGetAsEntry — fall back to the snapshotted FileList.
    return fallbackFiles;
  }

  const results = await Promise.all(
    usable.map(async ({ item, entry }) => {
      try {
        if (entry!.isDirectory) {
          return await traverseEntry(entry, "");
        }
        // Top-level files via getAsFile: sync, keeps an empty webkitRelativePath.
        const file = item.getAsFile();
        if (file) return [file];
        return await traverseEntry(entry, "");
      } catch {
        if (entry?.isDirectory) return [];
        const file = item.getAsFile();
        return file ? [file] : [];
      }
    }),
  );

  // Once FileSystemEntry results exist, trust them (including empty arrays).
  // Empty folders / all-hidden files falling back to dataTransfer.files would
  // re-expose Chrome/Edge's size-0 ghost directory entries.
  return results.flat();
};

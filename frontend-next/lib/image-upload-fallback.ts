/* B-2a(3): per-image upload with inline-base64 fallback.
 *
 * Each file is tracked individually: successes yield attachment IDs, and
 * only the files whose upload actually failed fall back to inline base64.
 * (Slicing the original list by the success count misfires on mixed
 * results — e.g. first fails + second succeeds would inline the already
 * uploaded second image and drop the first.) Order follows the input. */

export interface ImageUploadFallbackResult {
  attachmentIds: string[];
  inlineImages?: Array<{ data: string }>;
}

export async function uploadImagesWithFallback(
  files: File[],
  upload: (file: File) => Promise<string>,
  toDataUri: (file: File) => Promise<string>,
): Promise<ImageUploadFallbackResult> {
  const attachmentIds: string[] = [];
  const failed: File[] = [];
  for (const file of files) {
    try {
      attachmentIds.push(await upload(file));
    } catch {
      failed.push(file);
    }
  }
  let inlineImages: Array<{ data: string }> | undefined;
  if (failed.length > 0) {
    inlineImages = [];
    for (const file of failed) {
      try {
        inlineImages.push({ data: await toDataUri(file) });
      } catch {
        /* skip unreadable files rather than failing the turn */
      }
    }
    if (inlineImages.length === 0) inlineImages = undefined;
  }
  return { attachmentIds, inlineImages };
}

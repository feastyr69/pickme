import { supabase } from "./supabase";

const api = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

type PresignResponse = {
  photo_id: string;
  thumb_upload_url: string;
  preview_upload_url: string;
  original_upload_url: string;
  thumb_url: string;
  preview_url: string;
  original_key: string;
};

export type UploadedPhoto = {
  id: string;
  group_id: string;
  thumb_url: string;
  preview_url: string;
  original_key: string;
  status: string;
  created_at: string;
};

export async function uploadPhoto(file: File, groupId: string): Promise<UploadedPhoto> {
  if (!supabase) throw new Error("Supabase is not configured");
  if (!file.type || file.size <= 0 || file.size > 10 * 1024 * 1024) {
    throw new Error("Choose a non-empty image no larger than 10 MB");
  }

  const {
    data: { session },
    error: sessionError,
  } = await supabase.auth.getSession();
  if (sessionError) throw sessionError;
  if (!session?.access_token) throw new Error("Sign in before uploading photos");
  const headers = { Authorization: `Bearer ${session.access_token}` };
  const [thumb, preview] = await Promise.all([toWebP(file, 400, 0.75), toWebP(file, 1200, 0.85)]);

  const presignResponse = await fetch(`${api}/api/uploads/presign`, {
    method: "POST",
    headers: { ...headers, "Content-Type": "application/json" },
    body: JSON.stringify({
      group_id: groupId,
      original_content_type: file.type,
      original_size: file.size,
    }),
  });
  const presign = await parseResponse<PresignResponse>(presignResponse);

  const uploads = [
    putFile(presign.thumb_upload_url, thumb, "image/webp"),
    putFile(presign.preview_upload_url, preview, "image/webp"),
    putFile(presign.original_upload_url, file, file.type),
  ];
  await Promise.all(uploads);

  const completeResponse = await fetch(`${api}/api/uploads/complete`, {
    method: "POST",
    headers: { ...headers, "Content-Type": "application/json" },
    body: JSON.stringify({
      photo_id: presign.photo_id,
      group_id: groupId,
      thumb_url: presign.thumb_url,
      preview_url: presign.preview_url,
      original_key: presign.original_key,
    }),
  });
  return parseResponse<UploadedPhoto>(completeResponse);
}

async function putFile(url: string, file: Blob, contentType: string): Promise<void> {
  const response = await fetch(url, {
    method: "PUT",
    headers: { "Content-Type": contentType },
    body: file,
  });
  if (!response.ok) throw new Error(`Storage upload failed (${response.status})`);
}

async function toWebP(file: File, maxWidth: number, quality: number): Promise<Blob> {
  const image = await createImageBitmap(file);
  try {
    const scale = Math.min(1, maxWidth / image.width);
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.width * scale));
    canvas.height = Math.max(1, Math.round(image.height * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Could not create image canvas");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        (blob) => (blob ? resolve(blob) : reject(new Error("Could not encode WebP image"))),
        "image/webp",
        quality,
      );
    });
  } finally {
    image.close();
  }
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || `Request failed (${response.status})`);
  }
  return response.json() as Promise<T>;
}

"use client";

import { ChangeEvent, useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { supabase } from "../../../lib/supabase";
import { UploadedPhoto, uploadPhoto } from "../../../lib/uploadPhoto";

type Group = { id: string; name: string; created_at: string };
type FaceGroup = {
  id: string;
  label: string;
  photo_count: number;
  preview_url: string;
  bbox: number[];
};
type Overview = {
  face_groups: FaceGroup[];
  photo_count: number;
  no_face_count: number;
  unprocessed_count: number;
  pending_photo_ids: string[];
  processing_photo_ids: string[];
  failed_photo_ids: string[];
};
type UploadItem = {
  id: string;
  name: string;
  photoId?: string;
  state: "queued" | "uploading" | "processing" | "done" | "error";
  message?: string;
};

const api = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const allowedTypes = new Set(["image/jpeg", "image/png", "image/webp"]);
const maxFileSize = 10 * 1024 * 1024;
const emptyOverview: Overview = {
  face_groups: [],
  photo_count: 0,
  no_face_count: 0,
  unprocessed_count: 0,
  pending_photo_ids: [],
  processing_photo_ids: [],
  failed_photo_ids: [],
};

export default function GroupGalleryPage() {
  const params = useParams<{ id: string }>();
  const groupId = params.id;
  const router = useRouter();
  const [group, setGroup] = useState<Group | null>(null);
  const [overview, setOverview] = useState<Overview>(emptyOverview);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [uploading, setUploading] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [previewDimensions, setPreviewDimensions] = useState<Record<string, [number, number]>>({});

  const activePhotoIDs = useMemo(
    () => new Set([...overview.pending_photo_ids, ...overview.processing_photo_ids]),
    [overview.pending_photo_ids, overview.processing_photo_ids],
  );

  const loadOverview = useCallback(
    async (signal?: AbortSignal) => {
      if (!supabase) throw new Error("Supabase is not configured.");
      const { data, error: sessionError } = await supabase.auth.getSession();
      if (sessionError) throw sessionError;
      const token = data.session?.access_token;
      if (!token) {
        router.replace("/");
        return;
      }
      const response = await fetch(`${api}/api/groups/${encodeURIComponent(groupId)}/face-groups`, {
        headers: { Authorization: `Bearer ${token}` },
        signal,
      });
      if (!response.ok) throw new Error((await response.text()) || "Could not load face groups.");
      const result = (await response.json()) as Overview;
      setOverview(result);
      setUploads((current) =>
        current.map((upload) => {
          if (!upload.photoId) return upload;
          if (result.failed_photo_ids.includes(upload.photoId)) {
            return { ...upload, state: "error", message: "Photo processing failed" };
          }
          if (
            result.pending_photo_ids.includes(upload.photoId) ||
            result.processing_photo_ids.includes(upload.photoId)
          ) {
            return { ...upload, state: "processing", message: "Finding faces…" };
          }
          if (upload.state === "processing")
            return { ...upload, state: "done", message: "Ready in a collection" };
          return upload;
        }),
      );
    },
    [groupId, router],
  );

  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      if (!supabase) {
        setError("Supabase is not configured.");
        setLoading(false);
        return;
      }
      try {
        const { data, error: sessionError } = await supabase.auth.getSession();
        if (sessionError) throw sessionError;
        const token = data.session?.access_token;
        if (!token) {
          router.replace("/");
          return;
        }
        const response = await fetch(`${api}/api/groups`, {
          headers: { Authorization: `Bearer ${token}` },
          signal: controller.signal,
        });
        if (!response.ok) throw new Error((await response.text()) || "Could not load your groups.");
        const groups = (await response.json()) as Group[];
        const currentGroup = groups.find((item) => item.id === groupId);
        if (!currentGroup) throw new Error("This group could not be found.");
        setGroup(currentGroup);
        await loadOverview(controller.signal);
      } catch (loadError) {
        if (!controller.signal.aborted) {
          setError(loadError instanceof Error ? loadError.message : "Could not load this group.");
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [groupId, loadOverview, router]);

  useEffect(() => {
    if (activePhotoIDs.size === 0) return;
    const interval = window.setInterval(() => {
      void loadOverview().catch((pollError: unknown) => {
        console.error("Could not refresh photo processing status", pollError);
      });
    }, 3000);
    return () => window.clearInterval(interval);
  }, [activePhotoIDs, loadOverview]);

  async function handleFilesSelected(event: ChangeEvent<HTMLInputElement>) {
    const input = event.currentTarget;
    const files = Array.from(input.files ?? []);
    input.value = "";
    if (!files.length) return;

    const jobs = files.map((file) => ({ id: crypto.randomUUID(), file }));
    const initial = jobs.map(({ id, file }): UploadItem => {
      if (!allowedTypes.has(file.type))
        return {
          id,
          name: file.name,
          state: "error",
          message: "Choose a JPEG, PNG, or WebP image.",
        };
      if (file.size <= 0 || file.size > maxFileSize)
        return {
          id,
          name: file.name,
          state: "error",
          message: "Image must be no larger than 10 MB.",
        };
      return { id, name: file.name, state: "queued" };
    });
    setUploads((current) => [...current, ...initial]);
    const valid = jobs.filter(
      ({ file }) => allowedTypes.has(file.type) && file.size > 0 && file.size <= maxFileSize,
    );
    if (!valid.length) return;

    setUploading(true);
    for (const { id, file } of valid) {
      setUploads((current) =>
        current.map((upload) =>
          upload.id === id
            ? { ...upload, state: "uploading", message: "Uploading originals and previews…" }
            : upload,
        ),
      );
      try {
        const photo: UploadedPhoto = await uploadPhoto(file, groupId);
        setUploads((current) =>
          current.map((upload) =>
            upload.id === id
              ? { ...upload, photoId: photo.id, state: "processing", message: "Finding faces…" }
              : upload,
          ),
        );
        setOverview((current) => ({
          ...current,
          pending_photo_ids: [...new Set([...current.pending_photo_ids, photo.id])],
        }));
      } catch (uploadError) {
        setUploads((current) =>
          current.map((upload) =>
            upload.id === id
              ? {
                  ...upload,
                  state: "error",
                  message:
                    uploadError instanceof Error
                      ? uploadError.message
                      : "Could not upload this photo.",
                }
              : upload,
          ),
        );
      }
    }
    setUploading(false);
    void loadOverview().catch(() => undefined);
  }

  async function signOut() {
    await supabase?.auth.signOut();
    router.replace("/");
  }

  async function deleteGroup() {
    if (
      !group ||
      !window.confirm(`Delete “${group.name}” and all its photos? This cannot be undone.`)
    ) {
      return;
    }
    setDeleting(true);
    setDeleteError("");
    try {
      if (!supabase) throw new Error("Supabase is not configured.");
      const { data, error: sessionError } = await supabase.auth.getSession();
      if (sessionError) throw sessionError;
      const token = data.session?.access_token;
      if (!token) throw new Error("Sign in before deleting this group.");
      const response = await fetch(`${api}/api/groups/${encodeURIComponent(groupId)}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!response.ok) {
        throw new Error((await response.text()) || "Could not delete this group.");
      }
      router.replace("/groups");
    } catch (deleteFailure) {
      setDeleteError(
        deleteFailure instanceof Error ? deleteFailure.message : "Could not delete this group.",
      );
    } finally {
      setDeleting(false);
    }
  }

  const totalPhotos = overview.photo_count;
  const processingCount = activePhotoIDs.size;

  return (
    <main className="groups-shell">
      <header className="topbar">
        <Link className="brand" href="/groups" aria-label="pickME groups">
          pick<span>ME</span>
        </Link>
        <button className="text-button" onClick={signOut} type="button">
          Sign out
        </button>
      </header>
      <section className="gallery-content" aria-labelledby="gallery-title">
        <Link className="back-link" href="/groups">
          <span aria-hidden="true">←</span> All groups
        </Link>
        {loading ? (
          <p className="state" role="status">
            Loading your group…
          </p>
        ) : error ? (
          <div className="gallery-error" role="alert">
            <h1>Couldn’t open this group</h1>
            <p>{error}</p>
            <Link className="upload-button" href="/groups">
              Back to groups
            </Link>
          </div>
        ) : group ? (
          <>
            <div className="gallery-heading">
              <div>
                <p className="eyebrow">YOUR SHARED MOMENTS</p>
                <h1 id="gallery-title">{group.name}</h1>
                <p className="subtext">
                  {totalPhotos} {totalPhotos === 1 ? "photo" : "photos"} in collections
                  {processingCount > 0 && (
                    <span className="processing-note"> · {processingCount} processing</span>
                  )}
                </p>
              </div>
              <div className="gallery-add">
                <input
                  id="gallery-file-input"
                  className="visually-hidden file-input"
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  aria-label="Choose photos to add to this group"
                  disabled={uploading || deleting}
                  multiple
                  onChange={handleFilesSelected}
                />
                <label className="primary-button gallery-add-button" htmlFor="gallery-file-input">
                  <span aria-hidden="true">＋</span>{" "}
                  {uploading ? "Uploading photos…" : "Add photos"}
                </label>
                <p>JPEG, PNG, or WebP · up to 10 MB each</p>
                <button
                  className="danger-button"
                  type="button"
                  disabled={deleting || uploading}
                  onClick={() => void deleteGroup()}
                >
                  {deleting ? "Deleting group…" : "Delete group"}
                </button>
              </div>
            </div>

            {deleteError && (
              <p className="error" role="alert">
                {deleteError}
              </p>
            )}
            {overview.failed_photo_ids.length > 0 && (
              <p className="processing-error" role="status">
                {overview.failed_photo_ids.length}{" "}
                {overview.failed_photo_ids.length === 1 ? "photo needs" : "photos need"} to be
                uploaded again because processing failed.
              </p>
            )}
            {uploads.length > 0 && (
              <ul
                className="upload-queue"
                aria-label="Photo upload and processing status"
                aria-live="polite"
              >
                {uploads.map((upload) => (
                  <li className={`upload-queue-item upload-${upload.state}`} key={upload.id}>
                    <span className="upload-queue-name">{upload.name}</span>
                    <span className="upload-queue-status">{upload.message ?? upload.state}</span>
                  </li>
                ))}
              </ul>
            )}

            <div className="collection-grid" aria-label="Photo collections">
              {overview.face_groups.map((face) => {
                const [x1 = 0, y1 = 0, x2 = 0, y2 = 0] = face.bbox;
                const [width = 0, height = 0] = previewDimensions[face.id] ?? [];
                const x =
                  width > 0 ? Math.max(0, Math.min(100, ((x1 + x2) / 2 / width) * 100)) : 50;
                const y =
                  height > 0 ? Math.max(0, Math.min(100, ((y1 + y2) / 2 / height) * 100)) : 50;
                return (
                  <Link
                    className="collection-card"
                    href={`/groups/${encodeURIComponent(groupId)}/gallery/${encodeURIComponent(face.id)}`}
                    key={face.id}
                  >
                    <div className="collection-cover face-cover">
                      <img
                        src={face.preview_url}
                        alt=""
                        loading="lazy"
                        onLoad={(event) => {
                          const { naturalWidth, naturalHeight } = event.currentTarget;
                          setPreviewDimensions((current) => ({
                            ...current,
                            [face.id]: [naturalWidth, naturalHeight],
                          }));
                        }}
                        style={{ objectPosition: `${x}% ${y}%` }}
                      />
                      <span className="collection-badge">{face.photo_count}</span>
                    </div>
                    <div className="collection-card-copy">
                      <h2>{face.label}</h2>
                      <p>
                        {face.photo_count} {face.photo_count === 1 ? "photo" : "photos"}
                      </p>
                    </div>
                  </Link>
                );
              })}
              {overview.no_face_count > 0 && (
                <Link
                  className="collection-card"
                  href={`/groups/${encodeURIComponent(groupId)}/gallery/no-faces`}
                >
                  <div className="collection-cover no-face-cover">
                    <span aria-hidden="true">◇</span>
                    <span className="collection-badge">{overview.no_face_count}</span>
                  </div>
                  <div className="collection-card-copy">
                    <h2>No faces</h2>
                    <p>
                      {overview.no_face_count} {overview.no_face_count === 1 ? "photo" : "photos"}
                    </p>
                  </div>
                </Link>
              )}
              {overview.unprocessed_count > 0 && (
                <Link
                  className="collection-card"
                  href={`/groups/${encodeURIComponent(groupId)}/gallery/unprocessed`}
                >
                  <div className="collection-cover unprocessed-cover">
                    <span aria-hidden="true">…</span>
                    <span className="collection-badge">{overview.unprocessed_count}</span>
                  </div>
                  <div className="collection-card-copy">
                    <h2>Unprocessed</h2>
                    <p>Older photos not face grouped</p>
                  </div>
                </Link>
              )}
            </div>
            {overview.face_groups.length === 0 &&
              overview.no_face_count === 0 &&
              overview.unprocessed_count === 0 &&
              processingCount === 0 && (
                <div className="gallery-empty">
                  <span className="empty-mark" aria-hidden="true">
                    ✳
                  </span>
                  <h2>Every great gallery starts with one photo</h2>
                  <p>Add photos to find people and group the moments they share.</p>
                </div>
              )}
            {processingCount > 0 && (
              <p className="state processing-state" role="status">
                Photos are processing in the background. Collections update automatically.
              </p>
            )}
          </>
        ) : null}
      </section>
    </main>
  );
}

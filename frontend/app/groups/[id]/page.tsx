"use client";

import { ChangeEvent, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { supabase } from "../../../lib/supabase";
import { uploadPhoto } from "../../../lib/uploadPhoto";

type Group = { id: string; name: string; created_at: string };
type GalleryPhoto = {
  id: string;
  group_id: string;
  thumb_url: string;
  preview_url: string;
  status: string;
  created_at: string;
};
type UploadItem = {
  id: string;
  name: string;
  state: "queued" | "uploading" | "done" | "error";
  message?: string;
};

const api = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const allowedTypes = new Set(["image/jpeg", "image/png", "image/webp"]);
const maxFileSize = 10 * 1024 * 1024;

export default function GroupGalleryPage() {
  const params = useParams<{ id: string }>();
  const groupId = params.id;
  const router = useRouter();
  const [group, setGroup] = useState<Group | null>(null);
  const [photos, setPhotos] = useState<GalleryPhoto[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [uploading, setUploading] = useState(false);
  const [activePhoto, setActivePhoto] = useState<GalleryPhoto | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    let cancelled = false;

    async function loadGroup() {
      if (!supabase) {
        setError("Supabase is not configured.");
        setLoading(false);
        return;
      }

      try {
        const { data, error: sessionError } = await supabase.auth.getSession();
        if (sessionError) throw sessionError;
        const accessToken = data.session?.access_token;
        if (!accessToken) {
          router.replace("/");
          return;
        }

        const headers = { Authorization: `Bearer ${accessToken}` };
        const [groupsResponse, photosResponse] = await Promise.all([
          fetch(`${api}/api/groups`, { headers }),
          fetch(`${api}/api/groups/${encodeURIComponent(groupId)}/photos`, { headers }),
        ]);
        if (!groupsResponse.ok) throw new Error(await groupsResponse.text());
        if (!photosResponse.ok) throw new Error(await photosResponse.text());

        const groups = (await groupsResponse.json()) as Group[];
        const savedPhotos = (await photosResponse.json()) as GalleryPhoto[];
        if (cancelled) return;

        const currentGroup = groups.find((item) => item.id === groupId);
        if (!currentGroup) {
          setError("This group could not be found.");
          return;
        }

        setGroup(currentGroup);
        setPhotos(savedPhotos);
      } catch (loadError) {
        if (!cancelled) {
          setError(loadError instanceof Error ? loadError.message : "Could not load this group.");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void loadGroup();
    return () => {
      cancelled = true;
    };
  }, [groupId, router]);

  useEffect(() => {
    if (!activePhoto) return;

    const previousFocus = document.activeElement;
    closeButtonRef.current?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setActivePhoto(null);
      if (event.key === "Tab") {
        event.preventDefault();
        closeButtonRef.current?.focus();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      if (previousFocus instanceof HTMLElement) previousFocus.focus();
    };
  }, [activePhoto]);

  const updateUpload = useCallback((id: string, patch: Partial<UploadItem>) => {
    setUploads((current) =>
      current.map((upload) => (upload.id === id ? { ...upload, ...patch } : upload)),
    );
  }, []);

  async function handleFilesSelected(event: ChangeEvent<HTMLInputElement>) {
    const input = event.currentTarget;
    const files = Array.from(input.files ?? []);
    input.value = "";
    if (!files.length) return;

    const jobs = files.map((file) => ({ id: crypto.randomUUID(), file }));
    const initialUploads: UploadItem[] = jobs.map(({ id, file }) => {
      if (!allowedTypes.has(file.type)) {
        return {
          id,
          name: file.name,
          state: "error",
          message: "Choose a JPEG, PNG, or WebP image.",
        };
      }
      if (file.size <= 0 || file.size > maxFileSize) {
        return {
          id,
          name: file.name,
          state: "error",
          message: "Image must be no larger than 10 MB.",
        };
      }
      return { id, name: file.name, state: "queued" };
    });
    setUploads((current) => [...current, ...initialUploads]);

    const validJobs = jobs.filter((job) => {
      const file = job.file;
      return allowedTypes.has(file.type) && file.size > 0 && file.size <= maxFileSize;
    });
    if (!validJobs.length) return;

    setUploading(true);
    for (const { id, file } of validJobs) {
      updateUpload(id, { state: "uploading" });
      try {
        const photo = await uploadPhoto(file, groupId);
        setPhotos((current) => [photo, ...current.filter((item) => item.id !== photo.id)]);
        updateUpload(id, { state: "done", message: "Added to gallery" });
      } catch (uploadError) {
        updateUpload(id, {
          state: "error",
          message:
            uploadError instanceof Error ? uploadError.message : "Could not upload this photo.",
        });
      }
    }
    setUploading(false);
  }

  async function signOut() {
    await supabase?.auth.signOut();
    router.replace("/");
  }

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
                  {photos.length} {photos.length === 1 ? "photo" : "photos"}
                  {photos.length ? " in this group" : " so far"}
                </p>
              </div>
              <div className="gallery-add">
                <input
                  id="gallery-file-input"
                  className="visually-hidden file-input"
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  aria-label="Choose photos to add to this group"
                  disabled={uploading}
                  multiple
                  onChange={handleFilesSelected}
                />
                <label className="primary-button gallery-add-button" htmlFor="gallery-file-input">
                  <span aria-hidden="true">＋</span>{" "}
                  {uploading ? "Uploading photos…" : "Add photos"}
                </label>
                <p>JPEG, PNG, or WebP · up to 10 MB each</p>
              </div>
            </div>

            {uploads.length > 0 && (
              <ul className="upload-queue" aria-label="Photo upload status" aria-live="polite">
                {uploads.map((upload) => (
                  <li className={`upload-queue-item upload-${upload.state}`} key={upload.id}>
                    <span className="upload-queue-name">{upload.name}</span>
                    <span className="upload-queue-status">
                      {upload.state === "queued" && "Waiting"}
                      {upload.state === "uploading" && "Uploading…"}
                      {upload.state === "done" && upload.message}
                      {upload.state === "error" && upload.message}
                    </span>
                  </li>
                ))}
              </ul>
            )}

            {photos.length === 0 ? (
              <div className="gallery-empty">
                <span className="empty-mark" aria-hidden="true">
                  ✳
                </span>
                <h2>Every great gallery starts with one photo</h2>
                <p>Add the first moment from this event. You can select several photos at once.</p>
              </div>
            ) : (
              <div className="photo-gallery" aria-label={`${group.name} photo gallery`}>
                {photos.map((photo) => (
                  <button
                    className="gallery-photo"
                    key={photo.id}
                    type="button"
                    onClick={() => setActivePhoto(photo)}
                    aria-label={`View photo from ${new Date(photo.created_at).toLocaleDateString()}`}
                  >
                    <img
                      src={photo.thumb_url}
                      alt={`Photo added ${new Date(photo.created_at).toLocaleDateString()}`}
                      loading="lazy"
                      decoding="async"
                    />
                    <span className="gallery-photo-date">
                      {new Date(photo.created_at).toLocaleDateString(undefined, {
                        month: "short",
                        day: "numeric",
                        year: "numeric",
                      })}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </>
        ) : null}
      </section>

      {activePhoto && (
        <div className="lightbox-backdrop" onMouseDown={() => setActivePhoto(null)}>
          <section
            className="lightbox"
            role="dialog"
            aria-modal="true"
            aria-label="Photo preview"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="lightbox-toolbar">
              <p>
                {new Date(activePhoto.created_at).toLocaleDateString(undefined, {
                  dateStyle: "long",
                })}
              </p>
              <button
                ref={closeButtonRef}
                className="lightbox-close"
                type="button"
                onClick={() => setActivePhoto(null)}
              >
                Close <span aria-hidden="true">×</span>
              </button>
            </div>
            <img
              className="lightbox-image"
              src={activePhoto.preview_url}
              alt={`Photo added ${new Date(activePhoto.created_at).toLocaleDateString()}`}
            />
          </section>
        </div>
      )}
    </main>
  );
}

"use client";

import {
  type ChangeEvent,
  type CSSProperties,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { supabase } from "../../../lib/supabase";
import { UploadedPhoto, uploadPhoto } from "../../../lib/uploadPhoto";

type Group = { id: string; name: string; created_at: string; is_owner: boolean };
type GroupMember = { user_id: string; email: string; is_owner: boolean };
type GroupInvitation = { id: string; created_at: string; expires_at: string };
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
const faceCoverAspectRatio = 1.28;
const targetFaceWidth = 0.62;
const targetFaceHeight = 0.7;
const minFaceCoverZoom = 1;
const maxFaceCoverZoom = 4;

function getFaceCoverStyle(
  bbox: number[],
  imageWidth: number,
  imageHeight: number,
): CSSProperties | undefined {
  if (bbox.length !== 4 || !bbox.every(Number.isFinite) || imageWidth <= 0 || imageHeight <= 0) {
    return undefined;
  }

  const [rawX1, rawY1, rawX2, rawY2] = bbox;
  const x1 = Math.max(0, Math.min(imageWidth, rawX1));
  const y1 = Math.max(0, Math.min(imageHeight, rawY1));
  const x2 = Math.max(0, Math.min(imageWidth, rawX2));
  const y2 = Math.max(0, Math.min(imageHeight, rawY2));
  if (x2 <= x1 || y2 <= y1) return undefined;

  const imageAspectRatio = imageWidth / imageHeight;
  const baseWidthPercent = Math.max(1, imageAspectRatio / faceCoverAspectRatio) * 100;
  const baseHeightPercent = Math.max(1, faceCoverAspectRatio / imageAspectRatio) * 100;
  const faceWidthPercent = ((x2 - x1) / imageWidth) * baseWidthPercent;
  const faceHeightPercent = ((y2 - y1) / imageHeight) * baseHeightPercent;
  const zoom = Math.max(
    minFaceCoverZoom,
    Math.min(
      maxFaceCoverZoom,
      Math.min(
        (targetFaceWidth * 100) / faceWidthPercent,
        (targetFaceHeight * 100) / faceHeightPercent,
      ),
    ),
  );
  const renderedWidthPercent = baseWidthPercent * zoom;
  const renderedHeightPercent = baseHeightPercent * zoom;
  const centerX = (x1 + x2) / 2;
  const centerY = (y1 + y2) / 2;
  const leftPercent = Math.max(
    100 - renderedWidthPercent,
    Math.min(0, 50 - (centerX / imageWidth) * renderedWidthPercent),
  );
  const topPercent = Math.max(
    100 - renderedHeightPercent,
    Math.min(0, 50 - (centerY / imageHeight) * renderedHeightPercent),
  );

  return {
    width: `${renderedWidthPercent}%`,
    height: `${renderedHeightPercent}%`,
    left: `${leftPercent}%`,
    top: `${topPercent}%`,
    objectFit: "fill",
    transform: "none",
  };
}

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
  const [members, setMembers] = useState<GroupMember[]>([]);
  const [invitations, setInvitations] = useState<GroupInvitation[]>([]);
  const [membershipError, setMembershipError] = useState("");
  const [inviteBusy, setInviteBusy] = useState(false);
  const [memberBusy, setMemberBusy] = useState("");
  const [inviteLink, setInviteLink] = useState("");
  const [shareInviteID, setShareInviteID] = useState("");
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
        if (currentGroup.is_owner) {
          const [membersResponse, invitesResponse] = await Promise.all([
            fetch(`${api}/api/groups/${encodeURIComponent(groupId)}/members`, {
              headers: { Authorization: `Bearer ${token}` },
              signal: controller.signal,
            }),
            fetch(`${api}/api/groups/${encodeURIComponent(groupId)}/invites`, {
              headers: { Authorization: `Bearer ${token}` },
              signal: controller.signal,
            }),
          ]);
          if (membersResponse.ok && invitesResponse.ok) {
            setMembers((await membersResponse.json()) as GroupMember[]);
            setInvitations((await invitesResponse.json()) as GroupInvitation[]);
          } else if (!controller.signal.aborted) {
            setMembershipError("Could not load group members and invitations.");
          }
        }
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

  useEffect(() => {
    if (invitations.length === 0) return;
    const nextExpiry = Math.min(...invitations.map((invite) => Date.parse(invite.expires_at)));
    const timeout = window.setTimeout(
      () => {
        const now = Date.now();
        const expiredIDs = new Set(
          invitations
            .filter((invite) => Date.parse(invite.expires_at) <= now)
            .map((invite) => invite.id),
        );
        setInvitations((current) =>
          current.filter((invite) => Date.parse(invite.expires_at) > now),
        );
        if (expiredIDs.has(shareInviteID)) {
          setInviteLink("");
          setShareInviteID("");
        }
      },
      Math.max(0, nextExpiry - Date.now() + 50),
    );
    return () => window.clearTimeout(timeout);
  }, [invitations, shareInviteID]);

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

  async function createInviteLink() {
    setInviteBusy(true);
    setMembershipError("");
    try {
      if (!supabase) throw new Error("Supabase is not configured.");
      const { data, error: sessionError } = await supabase.auth.getSession();
      if (sessionError) throw sessionError;
      const token = data.session?.access_token;
      if (!token) throw new Error("Sign in before creating an invite link.");
      const response = await fetch(`${api}/api/groups/${encodeURIComponent(groupId)}/invites`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!response.ok)
        throw new Error((await response.text()) || "Could not create an invite link.");
      const invitation = (await response.json()) as GroupInvitation & { token: string };
      const link = new URL(`/invite/${encodeURIComponent(invitation.token)}`, location.origin).href;
      setInvitations((current) => [invitation, ...current]);
      setInviteLink(link);
      setShareInviteID(invitation.id);
      try {
        await navigator.clipboard.writeText(link);
      } catch {
        setMembershipError("Invite created. Copy the link shown below to share it.");
      }
    } catch (inviteFailure) {
      setMembershipError(
        inviteFailure instanceof Error ? inviteFailure.message : "Could not create an invite link.",
      );
    } finally {
      setInviteBusy(false);
    }
  }

  async function copyInviteLink() {
    try {
      await navigator.clipboard.writeText(inviteLink);
      setMembershipError("Invite link copied.");
    } catch {
      setMembershipError("Could not access the clipboard. Select and copy the link manually.");
    }
  }

  async function removeMember(member: GroupMember) {
    if (!window.confirm(`Remove ${member.email || "this member"} from the group?`)) return;
    setMemberBusy(member.user_id);
    setMembershipError("");
    try {
      const { data, error: sessionError } = await supabase!.auth.getSession();
      if (sessionError) throw sessionError;
      const token = data.session?.access_token;
      if (!token) throw new Error("Sign in before managing group members.");
      const response = await fetch(
        `${api}/api/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(member.user_id)}`,
        { method: "DELETE", headers: { Authorization: `Bearer ${token}` } },
      );
      if (!response.ok) throw new Error((await response.text()) || "Could not remove member.");
      setMembers((current) => current.filter((item) => item.user_id !== member.user_id));
    } catch (removeFailure) {
      setMembershipError(
        removeFailure instanceof Error ? removeFailure.message : "Could not remove member.",
      );
    } finally {
      setMemberBusy("");
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
                {group.is_owner && (
                  <button
                    className="danger-button"
                    type="button"
                    disabled={deleting || uploading}
                    onClick={() => void deleteGroup()}
                  >
                    {deleting ? "Deleting group…" : "Delete group"}
                  </button>
                )}
              </div>
            </div>

            {group.is_owner && (
              <section className="member-management" aria-labelledby="members-title">
                <div className="member-management-heading">
                  <div>
                    <p className="eyebrow">GROUP ACCESS</p>
                    <h2 id="members-title">Members</h2>
                    <p>Members can view, upload, and download photos. You control this group.</p>
                  </div>
                  {invitations.length === 0 && (
                    <button
                      className="primary-button invite-create-button"
                      type="button"
                      disabled={inviteBusy}
                      onClick={() => void createInviteLink()}
                    >
                      {inviteBusy ? "Creating link…" : "Create invite link"}
                    </button>
                  )}
                </div>
                <p className="invite-link-note">
                  Anyone with a link can join until it expires. Each group has one active link at a
                  time; links expire after 7 days.
                </p>
                {invitations.length > 0 && (
                  <p className="invite-link-note" role="status">
                    An invite link is active until{" "}
                    {new Date(invitations[0].expires_at).toLocaleString()}. You can create a new one
                    after it expires.
                  </p>
                )}
                {inviteLink && (
                  <div className="invite-link-copy">
                    <label htmlFor="group-invite-link">Share this link</label>
                    <input
                      id="group-invite-link"
                      readOnly
                      value={inviteLink}
                      onFocus={(event) => event.currentTarget.select()}
                    />
                    <button
                      type="button"
                      className="text-button"
                      onClick={() => void copyInviteLink()}
                    >
                      Copy link
                    </button>
                  </div>
                )}
                {membershipError && (
                  <p className="error" role="alert">
                    {membershipError}
                  </p>
                )}
                <ul className="member-list" aria-label="Group members">
                  {members.map((member) => (
                    <li key={member.user_id}>
                      <span>{member.email || "Member"}</span>
                      {member.is_owner ? (
                        <span className="member-role">Owner</span>
                      ) : (
                        <button
                          className="member-remove-button"
                          type="button"
                          disabled={memberBusy === member.user_id}
                          onClick={() => void removeMember(member)}
                        >
                          {memberBusy === member.user_id ? "Removing…" : "Remove"}
                        </button>
                      )}
                    </li>
                  ))}
                </ul>
              </section>
            )}

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
                const [width = 0, height = 0] = previewDimensions[face.id] ?? [];
                const imageStyle = getFaceCoverStyle(face.bbox, width, height);
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
                        style={imageStyle}
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

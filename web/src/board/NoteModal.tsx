import { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { NOTE_COLORS, type LinkKind, type Note, type NoteColor } from '../api/protocol';
import { canEditNote, canInteract, useStore } from '../store/store';
import { StarButton } from './Sticky';
import './NoteModal.css';

const LINK_ICONS: Record<LinkKind, string> = { doc: '📄', slides: '📊', other: '🔗' };

interface DraftLink {
  title: string;
  url: string;
  kind: LinkKind;
}

interface Props {
  note: Note;
  onClose: () => void;
}

export function NoteModal({ note, onClose }: Props) {
  const author = useStore((s) => s.users[note.authorId]);
  const region = useStore((s) => s.regions.find((r) => r.id === note.regionId));
  const send = useStore((s) => s.send);
  const mayEdit = useStore((s) => canInteract(s) && canEditNote(s, note.authorId));
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !editing) onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [editing, onClose]);

  return (
    <div className="modal-backdrop" onPointerDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`modal note-modal note-modal-${note.color}`} role="dialog" aria-modal="true" aria-label={note.title}>
        {editing ? (
          <NoteEditor note={note} onDone={() => setEditing(false)} />
        ) : (
          <>
            <header className="note-modal-header">
              <h2>{note.title}</h2>
              <StarButton
                note={note}
                className="modal-star"
                onStar={(n) => send(n.starred ? 'unstar_note' : 'star_note', { noteId: n.id }).catch(() => {})}
              />
              <button className="icon" onClick={onClose} aria-label="Close">
                ✕
              </button>
            </header>
            <p className="note-modal-meta">
              Proposed by {author?.name ?? 'unknown'}
              {region && (
                <>
                  {' · '}
                  <span className="swatch-dot" style={{ background: region.color }} /> {region.label}
                </>
              )}
              {note.voteTotal > 0 && ` · ${note.voteTotal} vote${note.voteTotal === 1 ? '' : 's'}`}
            </p>
            {note.hidden && <p className="note-modal-hidden">Hidden by a moderator — participants can't see this note.</p>}
            <div className="note-modal-body markdown">
              {note.bodyMd.trim() ? (
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  components={{
                    a: ({ node: _node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer" />,
                  }}
                >
                  {note.bodyMd}
                </ReactMarkdown>
              ) : (
                <p className="muted">No details yet.</p>
              )}
            </div>
            {note.links.length > 0 && (
              <ul className="note-modal-links">
                {note.links.map((l) => (
                  <li key={l.id}>
                    <span aria-hidden>{LINK_ICONS[l.kind]}</span>{' '}
                    <a href={l.url} target="_blank" rel="noopener noreferrer">
                      {l.title}
                    </a>
                  </li>
                ))}
              </ul>
            )}
            {mayEdit && (
              <footer className="note-modal-actions">
                <DeleteButton note={note} onDeleted={onClose} />
                <span className="spacer" />
                <button className="primary" onClick={() => setEditing(true)}>
                  Edit
                </button>
              </footer>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function DeleteButton({ note, onDeleted }: { note: Note; onDeleted: () => void }) {
  const send = useStore((s) => s.send);
  const [confirming, setConfirming] = useState(false);
  if (!confirming) {
    return (
      <button className="danger-quiet" onClick={() => setConfirming(true)}>
        Delete…
      </button>
    );
  }
  return (
    <span className="confirm">
      Delete “{note.title}”?{' '}
      <button
        className="danger"
        onClick={() => {
          send('delete_note', { noteId: note.id }).then(onDeleted, () => setConfirming(false));
        }}
      >
        Delete
      </button>{' '}
      <button onClick={() => setConfirming(false)}>Keep</button>
    </span>
  );
}

function sameLinks(a: DraftLink[], b: DraftLink[]): boolean {
  return a.length === b.length && a.every((l, i) => l.title === b[i].title && l.url === b[i].url && l.kind === b[i].kind);
}

function NoteEditor({ note, onDone }: { note: Note; onDone: () => void }) {
  const send = useStore((s) => s.send);
  const [title, setTitle] = useState(note.title);
  const [body, setBody] = useState(note.bodyMd);
  const [color, setColor] = useState<NoteColor>(note.color);
  const original: DraftLink[] = note.links.map(({ title, url, kind }) => ({ title, url, kind }));
  const [links, setLinks] = useState<DraftLink[]>(original);
  const [busy, setBusy] = useState(false);

  const save = async () => {
    setBusy(true);
    try {
      const update: { noteId: string; title?: string; bodyMd?: string; color?: NoteColor } = { noteId: note.id };
      if (title.trim() !== note.title) update.title = title.trim();
      if (body !== note.bodyMd) update.bodyMd = body;
      if (color !== note.color) update.color = color;
      if (Object.keys(update).length > 1) await send('update_note', update);
      const cleaned = links.filter((l) => l.title.trim() || l.url.trim());
      if (!sameLinks(cleaned, original)) await send('set_links', { noteId: note.id, links: cleaned });
      onDone();
    } catch {
      setBusy(false); // shown as a toast; keep the draft
    }
  };

  const setLink = (i: number, patch: Partial<DraftLink>) =>
    setLinks((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  return (
    <form
      className="note-editor"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onDone();
      }}
    >
      <label>
        Title
        <input value={title} maxLength={120} required onChange={(e) => setTitle(e.target.value)} autoFocus />
      </label>
      <fieldset className="color-picker">
        <legend>Color</legend>
        {NOTE_COLORS.map((c) => (
          <label key={c} className={`swatch sticky-${c}${c === color ? ' selected' : ''}`} title={c}>
            <input type="radio" name="color" value={c} checked={c === color} onChange={() => setColor(c)} />
          </label>
        ))}
      </fieldset>
      <label>
        Details <span className="muted">(Markdown)</span>
        <textarea value={body} rows={10} maxLength={20000} onChange={(e) => setBody(e.target.value)} />
      </label>
      <fieldset className="link-editor">
        <legend>Links</legend>
        {links.map((l, i) => (
          <div className="link-row" key={i}>
            <select value={l.kind} onChange={(e) => setLink(i, { kind: e.target.value as LinkKind })} aria-label="Kind">
              <option value="doc">📄 Doc</option>
              <option value="slides">📊 Slides</option>
              <option value="other">🔗 Other</option>
            </select>
            <input placeholder="Title" value={l.title} onChange={(e) => setLink(i, { title: e.target.value })} />
            <input
              placeholder="https://…"
              type="url"
              value={l.url}
              onChange={(e) => setLink(i, { url: e.target.value })}
            />
            <button type="button" className="icon" aria-label="Remove link" onClick={() => setLinks((ls) => ls.filter((_, j) => j !== i))}>
              ✕
            </button>
          </div>
        ))}
        {links.length < 20 && (
          <button type="button" onClick={() => setLinks((ls) => [...ls, { title: '', url: '', kind: 'doc' }])}>
            + Add link
          </button>
        )}
      </fieldset>
      <footer className="note-modal-actions">
        <span className="spacer" />
        <button type="button" onClick={onDone} disabled={busy}>
          Cancel
        </button>
        <button className="primary" type="submit" disabled={busy || !title.trim()}>
          {busy ? 'Saving…' : 'Save'}
        </button>
      </footer>
    </form>
  );
}

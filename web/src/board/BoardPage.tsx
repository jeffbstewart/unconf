import { useEffect, useState } from 'react';
import { useStore } from '../store/store';
import { useView } from '../store/view';
import { Board } from './Board';
import { ListView } from './ListView';
import { NoteModal } from './NoteModal';
import { ViewBar } from './ViewBar';

/** The board screen: personal view bar, board or list, and the note modal. */
export function BoardPage() {
  const layout = useView((s) => s.layout);
  const notes = useStore((s) => s.notes);
  const [openNoteId, setOpenNoteId] = useState<string | null>(null);

  // Close the modal if its note disappears (deleted, or hidden from us).
  useEffect(() => {
    if (openNoteId && !notes[openNoteId]) setOpenNoteId(null);
  }, [notes, openNoteId]);

  return (
    <>
      <ViewBar />
      {layout === 'list' ? <ListView onOpen={setOpenNoteId} /> : <Board onOpen={setOpenNoteId} />}
      {openNoteId && notes[openNoteId] && <NoteModal note={notes[openNoteId]} onClose={() => setOpenNoteId(null)} />}
    </>
  );
}

import { useEffect, useState } from 'react';
import { useStore } from '../store/store';
import { LifecycleControl } from './LifecycleControl';
import './AdminPage.css';

/** Organizer controls. Moderation tools join in milestone 8. */
export function AdminPage() {
  const event = useStore((s) => s.event);
  const role = useStore((s) => s.you?.role);
  if (!event) return null;
  if (role !== 'organizer') {
    return <p className="app-message">Only organizers can change event settings.</p>;
  }
  return (
    <main className="admin">
      <section className="admin-card">
        <h2>Event</h2>
        <dl>
          <dt>Name</dt>
          <dd>{event.name}</dd>
          <dt>Stage</dt>
          <dd>
            <LifecycleControl lifecycle={event.lifecycle} isOrganizer />
          </dd>
        </dl>
      </section>
      <VotingSettings />
    </main>
  );
}

function VotingSettings() {
  const event = useStore((s) => s.event)!;
  const send = useStore((s) => s.send);
  const [budget, setBudget] = useState(String(event.votesPerUser));
  useEffect(() => setBudget(String(event.votesPerUser)), [event.votesPerUser]);
  const done = event.lifecycle === 'done';
  const n = Number(budget);
  const budgetValid = Number.isInteger(n) && n >= 1 && n <= 20;

  return (
    <section className="admin-card">
      <h2>Voting</h2>
      <p className="muted">
        Participants place dots on the sessions they want. Close voting while breakouts run and reopen it between
        waves.
      </p>
      <div className="admin-row">
        <span className={`voting-status ${event.votingOpen ? 'open' : 'closed'}`}>
          Voting is {event.votingOpen ? 'open' : 'closed'}
        </span>
        <button
          className={event.votingOpen ? undefined : 'primary'}
          disabled={done}
          onClick={() => send('set_voting', { open: !event.votingOpen }).catch(() => {})}
        >
          {event.votingOpen ? 'Close voting' : 'Open voting'}
        </button>
      </div>
      <form
        className="admin-row"
        onSubmit={(e) => {
          e.preventDefault();
          if (budgetValid && n !== event.votesPerUser) send('set_votes_per_user', { n }).catch(() => {});
        }}
      >
        <label>
          Votes per person{' '}
          <input
            type="number"
            min={1}
            max={20}
            value={budget}
            disabled={done}
            onChange={(e) => setBudget(e.target.value)}
            aria-label="Votes per person"
          />
        </label>
        <button type="submit" disabled={done || !budgetValid || n === event.votesPerUser}>
          Save
        </button>
        <span className="muted">1–20. Lowering it keeps dots already placed.</span>
      </form>
    </section>
  );
}

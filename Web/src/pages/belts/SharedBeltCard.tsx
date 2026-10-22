import { Tag } from "../../components/Paper";
import { useDraftEditor } from "./draftEditor";
import { PoliciesForm } from "./PoliciesForm";

/**
 * The belt's card for the settings every trigger shares: its name, base branch, per-repository bases and policies.
 * Each box shows the draft's value and types into it; the daemon's context decides which of them the trigger uses.
 */
export function SharedBeltCard({ beltKey }: { beltKey: string }) {
  const editor = useDraftEditor();
  const { belt, context, edit, problemTarget } = editor;
  const showsBase = context?.triggerForm?.showsBase ?? true;
  // A shadow keeps live's name, base and repository bases, so both versions take the same work.
  const locked = editor.shadow === true;
  return <section className="belt-card belt-shared" aria-label="Belt">
    <FormHeading tag="Belt" title="Shared by every trigger" />
    {locked && <p className="field-note shadow-locked">The name, base branch and repository bases come from the live version. Change them there; the shadow follows.</p>}
    <fieldset className="shadow-locked-fields" disabled={locked}>
      <label className="belt-field"><span>Belt name</span>
        <input id={`belt-name-${beltKey}`} className="paper-field" placeholder="Belt name" {...problemTarget("belt", undefined, "name")} value={belt.name}
          onChange={event => { const value = event.target.value; edit(current => ({ ...current, name: value })); }} />
      </label>
      {showsBase && <label className="belt-field"><span>Base branch</span>
        <input className="paper-field" placeholder="Each repository's default" {...problemTarget("belt", undefined, "base")} value={belt.base}
          onChange={event => { const value = event.target.value; edit(current => ({ ...current, base: value })); }} />
      </label>}
      {showsBase && context?.triggerForm?.repositoryBases && <fieldset className="belt-repository-bases">
        <legend>{context.triggerForm.repositoryBases.heading}</legend>
        <p className="field-note">{context.triggerForm.repositoryBases.help}</p>
        {context.triggerForm.repositoryBases.fields.map(field => <label key={field.repository} className="belt-field"><span>{field.label}</span>
          <input className="paper-field" placeholder={context.triggerForm!.repositoryBases!.placeholder}
            {...problemTarget("belt", undefined, `repositoryBase:${field.repository}`)} value={belt.repositoryBases?.[field.repository] ?? ""}
            onChange={event => {
              const value = event.target.value;
              edit(current => {
                const bases = { ...(current.repositoryBases ?? {}) };
                if (value === "") delete bases[field.repository];
                else bases[field.repository] = value;
                return { ...current, repositoryBases: bases };
              });
            }} />
        </label>)}
      </fieldset>}
    </fieldset>
    <PoliciesForm />
  </section>;
}

/** A form card's heading: its bracketed tag over its title. */
function FormHeading({ tag, title }: { tag: string; title: string }) {
  return <div className="form-heading"><Tag>{tag}</Tag><h3>{title}</h3></div>;
}

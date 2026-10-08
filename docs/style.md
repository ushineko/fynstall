# How this is written

The documents in `docs/` and the README are written in what this repository
calls **plain technical English**.

The rules come from fynedesygn's `docs/style.md`, which takes them from
ASD-STE100, the Simplified Technical English specification written for
aerospace maintenance manuals. They are not that specification. STE controls
its vocabulary with a licensed dictionary, caps a procedural sentence at
twenty words, and allows one idea per sentence. This repository does none of
those three. **Nothing here should claim to be STE.** A reader who checks it
against the standard will find that it does not comply.

## The rules

**Use plain verbs, and use one verb for one meaning.** `reads`, `writes`,
`returns`, `draws`. Not `takes`, `speaks`, `grabs`, `punches`. A verb that
carries a metaphor is a verb the reader must translate first.

**Write in the active voice.** "The engine writes the receipt", not "the
receipt is written by the engine".

**Write in the simple present.** The behaviour is a fact about the program,
not a story about the day somebody found it.

**No idiom, no metaphor, no understatement.** "A table with a bend in it" and
"the honest answer" read well and cost a non-native reader a stop. Say what
happens.

**No asides in em-dashes, and no stacked subordinate clauses.** One sentence
may join two clauses with `and`, `so`, `because` or a semicolon. It should not
join four.

**Aim for twenty-five words and stop at thirty.** This is a ceiling, not a
target. A compound sentence that keeps cause and effect together is better
than two sentences that separate them.

**Keep the bold lead sentence.** It makes a long table skimmable, and plain
language does not mean flat formatting.

**Name the thing the same way every time.** A payload is a payload
everywhere. It is not a "bundle" in one paragraph and "assets" in the next.

**Define the words this project uses in its own sense.** The *builder*, the
*installer*, the *uninstaller*, the *payload*, the *manifest*, the *plan*, the
*receipt*, the *journal*, the *scope* and the *front end* are fynstall's
terms. Use them, and define each one where a reader first meets it.

## What is not converted

**The specifications in `specs/`.** They record what was decided and why, at
the time it was decided. Rewriting them would change the record.

**The changelog in `README.md`.** It is the same kind of record.

**Doc comments in Go source.** They carry argument rather than instruction,
and that is the part these rules serve least well. A comment here says why a
constant is the number it is, or which bug a test was written for, and the
reasoning is the payload. The exemption is about fit, not about protecting a
house voice.

## What is checked

Nothing is checked by a test yet. The sibling projects hold the mechanical
rules (the sentence ceiling and a list of empty words) in a `style_test.go`.
This repository adds one when it has documents in `docs/` beyond this page.

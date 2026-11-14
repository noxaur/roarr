# Transcode

`internal/transcode` converts imported files to AV1 in the background. The
queue is fed by the import history of Sonarr and Radarr and, for native
imports, by the after-import hook. One rule shapes it: the original is the user's file, and no failure, crash or
race may lose it or a file that replaced it.

## The swap: move aside, link in, delete last

A plain rename of the result over the original was rejected. Sonarr and
Radarr can replace the file at any moment, for example with an upgrade, and
a rename would silently overwrite whatever is there by then.

So `swap` first moves the original aside to a hidden backup next to it and
checks that the file it moved is the very file that was encoded (same file,
size and modification time). If it is not, the newer file goes back
untouched. The result is then hard-linked to the path, which fails rather
than overwrites when a file has appeared in the gap. The backup is deleted
only once the result is in place.

- No hard links (SMB, exFAT, some FUSE): the result is moved in with a
  rename that refuses to overwrite. Where the platform has no such rename,
  the job fails and the original is put back, or kept as the backup when
  that cannot be done safely either. A check followed by a plain rename was
  rejected: a file can appear between the two.
- The backup can never be put back over a newer file. Then both are kept and
  the job's message names the backup.
- An existing backup is never overwritten. A later job for the same path
  fails until the backup is removed by hand.

## Recovery after a crash

Between moving the original aside and linking the result in, the path is
empty. If roarr dies there, `Loop` repairs it before it starts any job:

| On disk | Action | Why it is safe |
| --- | --- | --- |
| Backup, nothing at the path | Put the backup back, without overwriting | The original is the only copy |
| Backup, and the temporary file is a hard link of the file at the path | Delete the backup, finish the job | Only `swap` links the temporary name to the path, so that file is the result |
| Backup and the path are the same file | Delete the backup | A restore by hard link was cut short, the backup is a second name |
| Backup, and any other file at the path | Keep both, fail the waiting job | It may be the result, or a file Sonarr/Radarr put there |

The second row is why `swap` deletes the backup before the temporary file:
while a backup exists next to a linked-in result, the proof still exists
too. The proof is missing on filesystems without hard links, where the
result is renamed in and the temporary name is gone with it. Recording the
result's identity in the state file before each swap would cover that, and
was rejected: it adds a write and a persisted field for a crash inside a
window of two system calls on a minority of filesystems, and the fallback
loses nothing.

Leftovers are looked for only beside the files of jobs in the state file.
Scanning the libraries was rejected: it costs a walk of every media folder
at each start, and a swap can only have happened to a file that has a job.
Temporary files of waiting jobs are deleted there as well, since nothing
runs yet.

## Shutdown

- Before the swap, a cancelled context puts the job back in the queue at
  every step. A shutdown is not a failure of the file, and a failed job
  does not run again by itself.
- The swap is never interrupted. `Loop` returns only after running jobs have
  returned, and `main` waits for it, so the process does not exit between
  the two renames. Jobs stop quickly because ffmpeg dies with the context.

## Holding a running job

Pause and the schedule suspend ffmpeg where it is, so no work is lost. Where
a process cannot be suspended, it is stopped and the job is queued again
rather than left running through the hold.

## Tools and the probe

ffmpeg and ffprobe are looked up once, by `verify.FindTools`, and the same
pair is given to both packages, so `FFMPEG_PATH` and `FFPROBE_PATH` apply to
conversion and verification alike. The reasons for the shared probe are in
[verify.md](verify.md). The decisions on a probed file (`Skip`, `Verify`,
`BuildArgs`) stay here.

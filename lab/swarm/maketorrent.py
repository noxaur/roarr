"""Create the lab pack torrent and the index mockindexer serves it from."""
import json, os, sys
from torf import Torrent

root = os.path.join(os.path.dirname(__file__), "..", "state")
payload = os.path.join(root, "swarm", "payload")
out = os.path.join(root, "torrents")
os.makedirs(out, exist_ok=True)
index = {}
for name in sorted(os.listdir(payload)):
    path = os.path.join(payload, name)
    if not os.path.isdir(path):
        continue
    t = Torrent(path=path, trackers=["http://seed1:9000/announce"], piece_size=256 * 1024, private=False)
    t.generate()
    fn = name + ".torrent"
    t.write(os.path.join(out, fn), overwrite=True)
    index[name] = {"file": fn, "size": t.size, "hash": t.infohash}
    print(name, t.infohash, t.size)
json.dump(index, open(os.path.join(out, "index.json"), "w"), indent=1)

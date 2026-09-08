"""Execute an exact named shell block from the fixed first-party control workflow."""
import pathlib,sys,subprocess,textwrap
p=pathlib.Path(__file__).resolve().parents[2]/'.github/workflows/ag03-qualification.yml'
s=p.read_text(); marker='      - name: '+sys.argv[1]+'\n'
assert s.count(marker)==1,sys.argv[1]
block=s.split(marker,1)[1].split('\n      - ',1)[0]
assert block.count('        run: |\n')==1
script=textwrap.dedent(block.split('        run: |\n',1)[1])
subprocess.run(['bash','-euo','pipefail','-c',script],check=True)

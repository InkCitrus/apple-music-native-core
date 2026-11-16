from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
def reports():
    p=ROOT/'build/reports';p.mkdir(parents=True,exist_ok=True);return p

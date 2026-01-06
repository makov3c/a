import sqlite3
import os

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
conn = sqlite3.connect(os.path.join(BASE_DIR, "baza.db"))
cursor = conn.cursor()

cursor.execute("SELECT name FROM sqlite_master WHERE type='table';")
tables = cursor.fetchall()

for table_name in tables:
    print(f"Contents of table: {table_name[0]}")
    cursor.execute(f"SELECT * FROM {table_name[0]};")
    rows = cursor.fetchall()
    for row in rows:
        print(row)

conn.close()
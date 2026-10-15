"""Verify bundled programming examples with small, independent reference checks.
Run: python3 backend/question_banks/verify_programming.py
These checks do not connect to Judge0 or execute submitted user programs.
"""
import json
import math
from pathlib import Path
from collections import deque


def expected(key, text):
    lines = text.strip().splitlines()
    nums = list(map(int, text.split())) if key not in ('algo-028', 'algo-031') else []
    if key == 'algo-025':
        assert nums[0] == len(nums) - 1
        return str(sum(nums[1:]))
    if key == 'algo-026':
        return str(math.gcd(*nums))
    if key == 'algo-027':
        n, x, *values = nums
        assert n == len(values) and values == sorted(values)
        return str(next((i for i, value in enumerate(values) if value >= x), n))
    if key == 'algo-028':
        # Repeated adjacent pair deletion is independent of the usual stack solution.
        value = text.strip()
        while True:
            shorter = value.replace('()', '').replace('[]', '').replace('{}', '')
            if value == shorter:
                return 'YES' if not value else 'NO'
            value = shorter
    if key == 'algo-029':
        n, q, *values = nums
        a, pairs = values[:n], values[n:]
        assert len(pairs) == q * 2
        return '\n'.join(str(sum(a[pairs[i]:pairs[i+1]+1])) for i in range(0, len(pairs), 2))
    if key == 'algo-030':
        a = nums[1:]
        assert nums[0] == len(a)
        return str(max(sum(a[i:j]) for i in range(len(a)) for j in range(i+1, len(a)+1)))
    if key == 'algo-031':
        h, w = map(int, lines[0].split())
        grid = lines[1:]
        assert len(grid) == h and all(len(row) == w for row in grid)
        if grid[0][0] == '#' or grid[-1][-1] == '#':
            return '-1'
        queue, distance = deque([(0, 0)]), {(0, 0): 0}
        while queue:
            row, col = queue.popleft()
            for r, c in ((row-1,col), (row+1,col), (row,col-1), (row,col+1)):
                if 0 <= r < h and 0 <= c < w and grid[r][c] == '.' and (r,c) not in distance:
                    distance[(r,c)] = distance[(row,col)] + 1
                    queue.append((r,c))
        return str(distance.get((h-1,w-1), -1))
    if key == 'algo-032':
        assert nums[0] == len(nums)-1
        return ' '.join(map(str, sorted(set(nums[1:]))))
    raise ValueError(key)


def main():
    questions = json.loads(Path(__file__).with_name('algorithms.json').read_text())
    count = 0
    for question in questions:
        if question['type'] != 'code':
            continue
        for case in question['judge']['testCases']:
            actual = expected(question['id'], case['input'])
            assert actual.strip() == case['expectedOutput'].strip(), (question['id'], actual)
            count += 1
    print(f'PASS: {count} programming cases checked across 8 problems')


if __name__ == '__main__':
    main()

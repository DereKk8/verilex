# The chain grammar

- A chain is words joined by `|`. Arguments follow the word: `item-stored apple`.
- Each word declares the states it `requires` and `provides`, and inherits those its claim pins; these encode the product's order of reality. verilex refuses a chain in which a word requires a state that no earlier word provides, before it starts anything:

  ```
  $ verilex run 'item-stored apple | store-open'
  verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it
  ```

- Agents are creative by composing existing words into new chains within those rules.

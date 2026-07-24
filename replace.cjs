const fs = require('fs');

const path = 'frontend/src/pages/Repository.jsx';
let content = fs.readFileSync(path, 'utf8');

const search = `  const calculateTreeStats = (nodes) => {
    nodes.forEach(node => {
      if (node.isDirectory) {
        calculateTreeStats(node.children);
        node.additions = node.children.reduce((sum, child) => sum + child.additions, 0);
        node.deletions = node.children.reduce((sum, child) => sum + child.deletions, 0);
      }
    });
  };

  const sortTree = (node) => {`;

const replace = `  const sortTree = (node) => {`;

if (!content.includes(search)) {
  console.log("NOT FOUND 1");
}

content = content.replace(search, replace);

const search2 = `      if (isLast) {
        child.additions = file.additions;
        child.deletions = file.deletions;
      }
    });
  });`;

const replace2 = `      if (isLast) {
        child.additions = file.additions;
        child.deletions = file.deletions;
      } else {
        // Pre-aggregate additions and deletions on parent directories to avoid recursive calculateTreeStats
        child.additions += file.additions;
        child.deletions += file.deletions;
      }
    });
  });`;

if (!content.includes(search2)) {
  console.log("NOT FOUND 2");
}

content = content.replace(search2, replace2);

const search3 = `  calculateTreeStats(root.children);
  sortTree(root);
  return root.children;
};`;

const replace3 = `  sortTree(root);
  return root.children;
};`;

if (!content.includes(search3)) {
  console.log("NOT FOUND 3");
}
content = content.replace(search3, replace3);

fs.writeFileSync(path, content);

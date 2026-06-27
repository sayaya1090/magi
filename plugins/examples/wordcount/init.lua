-- wordcount: an example magi plugin contributing a single tool.
-- It reads a file (requires the fs:read permission declared in plugin.toml)
-- and returns the number of words.

magi.register_tool{
  name = "wordcount",
  description = "Count the number of whitespace-separated words in a file.",
  schema = [[{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}]],
  execute = function(args)
    local content, err = magi.read_file(args.path)
    if content == nil then
      return ("could not read %s: %s"):format(args.path, err or "unknown"), true
    end
    local n = 0
    for _ in content:gmatch("%S+") do n = n + 1 end
    return ("%s has %d words"):format(args.path, n)
  end,
}

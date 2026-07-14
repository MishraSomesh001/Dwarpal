-- Sliding Window Rate Limiter
--
-- KEYS[1] = Rate limit key (e.g. tenant:user:/api)
--
-- ARGV[1] = Current timestamp (milliseconds)
-- ARGV[2] = Maximum allowed requests
-- ARGV[3] = Window duration (milliseconds)

local key = KEYS[1]

local now = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local window = tonumber(ARGV[3])

-- Remove requests outside the sliding window
local clear_before = now - window
redis.call("ZREMRANGEBYSCORE", key, "-inf", clear_before)

-- Count remaining requests
local current_requests = redis.call("ZCARD", key)

if current_requests < limit then
    -- Store current request timestamp
    redis.call("ZADD", key, now, now)

    -- Auto-delete inactive keys
    redis.call("PEXPIRE", key, window)

    return 1
else
    return 0
end
local GETBONUS_WEBHOOK_API_KEY = "***"

function getbonus_base_url(event)
    event.config.settings = event.config.settings or {}
    return event.config.settings.base_url or "https://stage.gb-platform.online"
end

function getbonus_header(headers, name)
    headers = headers or {}
    local expected = string.lower(name)
    for key, value in pairs(headers) do
        if string.lower(tostring(key)) == expected then
            return value
        end
    end
    return nil
end

function list(event)
    local response = http.request({
        method = "GET",
        url = getbonus_base_url(event) .. "/v1/partner/offers",
        headers = {
            ["X-Api-Key"] = event.config.secret
        }
    })
    if response.status >= 400 then
        return {
            ok = false,
            error = "partner_http_error",
            status = response.status,
            body = response.body
        }
    end
    local data = json.decode(response.body)
    local offers = data.offers or data.body or data.data or {}
    local tasks = {}
    for _, offer in ipairs(offers) do
        for _, step in ipairs(offer.steps or {}) do
            table.insert(tasks, {
                external_id = tostring(offer.id) .. ":" .. tostring(step.id),
                external_type = "step:" .. tostring(step.id),
                start_mode = "required",
                public_payload = {
                    provider = "getbonus",
                    offer_id = offer.id,
                    offer_title = offer.title,
                    step_id = step.id,
                    title = step.title,
                    description = step.description,
                    button_text = "Open"
                },
                private_payload = {
                    offer_id = offer.id,
                    step_id = step.id
                }
            })
        end
    end
    return {
        ok = true,
        tasks = tasks
    }
end

function start(event)
    local private = event.issue.private_payload or {}
    local click_id = event.issue.external_click_id
    if click_id == nil or click_id == "" then
        click_id = uuid.new()
    end
    local response = http.request({
        method = "POST",
        url = getbonus_base_url(event) .. "/v1/partner/click/generate",
        headers = {
            ["X-Api-Key"] = event.config.secret,
            ["Content-Type"] = "application/json"
        },
        body = json.encode({
            step_id = private.step_id,
            click_id = click_id
        })
    })
    if response.status == 409 then
        return {
            ok = false,
            error = "click_already_exists",
            retryable = true
        }
    end
    if response.status >= 400 then
        return {
            ok = false,
            error = "partner_http_error",
            status = response.status,
            body = response.body
        }
    end
    local data = json.decode(response.body)
    local body = data.body or data
    local external_click_id = body.external_click_id or click_id
    return {
        ok = true,
        started = true,
        status = "started",
        action_url = body.action_url,
        external_click_id = external_click_id,
        public_payload_patch = {
            action_url = body.action_url,
            button_text = "Open"
        },
        private_payload_patch = {
            external_click_id = external_click_id,
            action_url = body.action_url,
            step = body.step
        }
    }
end

function check(event)
    if event.issue.status == "completed" or event.issue.status == "claimed" then
        return {
            ok = true,
            completed = true,
            status = event.issue.status
        }
    end
    return {
        ok = true,
        completed = false,
        status = "not_completed"
    }
end

function callback(event)
    local request = event.request or {}
    local api_key = getbonus_header(request.headers, "X-Api-Key")
    local expected_api_key = GETBONUS_WEBHOOK_API_KEY
    if expected_api_key == nil or expected_api_key == "" or api_key ~= expected_api_key then
        return {
            ok = false,
            error = "invalid_api_key"
        }
    end

    local body = request.body or {}
    if body.event ~= "step_completed" then
        return {
            ok = false,
            error = "unsupported_event"
        }
    end
    return {
        ok = true,
        action = "complete",
        completed = true,
        external_click_id = body.external_click_id,
        status = "completed",
        payload = {
            provider = "getbonus",
            event = body.event,
            offer_id = body.offer_id,
            step_id = body.step_id,
            step_title = body.step_title,
            payout_usd = body.payout_usd,
            completed_at = body.completed_at
        }
    }
end

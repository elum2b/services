function subgram_base_url(event)
    event.config.settings = event.config.settings or {}
    return event.config.settings.base_url or "https://api.subgram.org"
end

function subgram_first_non_empty(...)
    for index = 1, select("#", ...) do
        local value = select(index, ...)
        if value ~= nil and tostring(value) ~= "" then
            return tostring(value)
        end
    end
    return ""
end

function subgram_partner_number(value)
    local number = tonumber(value)
    if number ~= nil then
        return number
    end
    return value
end

function subgram_config_string(event, key, fallback)
    event.config.settings = event.config.settings or {}
    local value = event.config.settings[key]
    if value == nil or tostring(value) == "" then
        return fallback
    end
    return tostring(value)
end

function list(event)
    event.variables = event.variables or {}
    event.config.settings = event.config.settings or {}
    local max_sponsors = event.limit
    if max_sponsors == nil or max_sponsors <= 0 then
        max_sponsors = tonumber(event.config.settings.max_sponsors) or 5
    end
    local chat_id = subgram_first_non_empty(event.variables.chat_id, event.identity.platform_user_id)
    local body = {
        chat_id = subgram_partner_number(chat_id),
        user_id = subgram_partner_number(event.identity.platform_user_id),
        language_code = event.locale,
        is_premium = event.identity.is_premium,
        action = subgram_config_string(event, "action", "task"),
        max_sponsors = max_sponsors,
        get_links = 1
    }
    if event.variables.first_name ~= nil and event.variables.first_name ~= "" then
        body.first_name = event.variables.first_name
    end
    local username = subgram_first_non_empty(event.variables.username, event.variables.tg_login)
    if username ~= "" then
        body.username = username
    end
    local response = http.request({
        method = "POST",
        url = subgram_base_url(event) .. "/get-sponsors",
        headers = {
            ["Auth"] = event.config.secret,
            ["Content-Type"] = "application/json"
        },
        body = json.encode(body)
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
    local sponsors = {}
    if data.additional ~= nil then
        sponsors = data.additional.sponsors or {}
    end
    local tasks = {}
    for _, sponsor in ipairs(sponsors) do
        if sponsor.link ~= nil and sponsor.link ~= "" and sponsor.available_now == true and sponsor.status ~=
            "subscribed" then
            local external_type = subgram_first_non_empty(sponsor.type, "resource")
            local ads_id = tostring(sponsor.ads_id)
            local resource_id = tostring(sponsor.resource_id or "")
            table.insert(tasks, {
                external_id = ads_id .. ":" .. resource_id,
                external_type = external_type,
                public_payload = {
                    ads_id = ads_id,
                    resource_id = resource_id,
                    link = sponsor.link,
                    button_text = subgram_first_non_empty(sponsor.button_text, "Подписаться"),
                    resource_logo = sponsor.resource_logo,
                    resource_name = sponsor.resource_name,
                    subgram_status = sponsor.status,
                    available_now = sponsor.available_now
                },
                private_payload = {
                    ads_id = ads_id,
                    resource_id = resource_id,
                    link = sponsor.link
                }
            })
        end
    end
    return {
        ok = true,
        tasks = tasks
    }
end

function subgram_callback_action(status)
    if status == "unsubscribe" or status == "unsubscribed" or status == "revoked" or status == "cancelled" or status ==
        "canceled" then
        return "revoked"
    end
    return "complete"
end

function subgram_callback_item(body)
    local status = subgram_first_non_empty(body.status, body.event, body.action, body.type)
    local action = subgram_callback_action(status)
    local external_id = subgram_first_non_empty(body.external_id, body.task_id, body.offer_id)
    if external_id == "" and body.ads_id ~= nil then
        external_id = tostring(body.ads_id) .. ":" .. tostring(body.resource_id or "")
    end
    return {
        action = action,
        status = status ~= "" and status or action,
        issue_id = body.issue_id,
        issue_ref = body.issue_ref or body.task_ref,
        external_click_id = body.external_click_id or body.click_id,
        external_id = external_id,
        platform_user_id = subgram_first_non_empty(body.platform_user_id, body.user_id, body.tg_user_id, body.chat_id),
        payload = {
            provider = "subgram",
            status = status,
            event = body.event,
            ads_id = body.ads_id,
            resource_id = body.resource_id,
            link = body.link,
            user_id = body.user_id
        }
    }
end

function callback(event)
    local request = event.request or {}
    local body = request.body or {}
    if body.webhooks ~= nil then
        local callbacks = {}
        for _, item in ipairs(body.webhooks or {}) do
            table.insert(callbacks, subgram_callback_item(item))
        end
        return {
            ok = true,
            callbacks = callbacks
        }
    end
    local result = subgram_callback_item(body)
    result.ok = true
    return result
end

function check(event)
    event.config.settings = event.config.settings or {}
    local private = event.issue.private_payload or {}
    local body = {
        user_id = subgram_partner_number(event.identity.platform_user_id)
    }
    if private.link ~= nil and private.link ~= "" then
        body.links = {private.link}
    end
    if private.ads_id ~= nil and private.ads_id ~= "" then
        local ads_id = tonumber(private.ads_id)
        if ads_id ~= nil then
            body.ads_ids = {ads_id}
        end
    end
    local response = http.request({
        method = "POST",
        url = subgram_base_url(event) .. "/get-user-subscriptions",
        headers = {
            ["Auth"] = event.config.secret,
            ["Content-Type"] = "application/json"
        },
        body = json.encode(body)
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
    local sponsors = {}
    if data.additional ~= nil then
        sponsors = data.additional.sponsors or {}
    end
    local status = "not_found"
    for _, sponsor in ipairs(sponsors) do
        if private.link == nil or private.link == "" or sponsor.link == nil or sponsor.link == "" or sponsor.link ==
            private.link then
            status = sponsor.status
            break
        end
    end
    local allow_notgetted = subgram_config_string(event, "allow_notgetted", "false") == "true"
    local completed = status == "subscribed" or (status == "notgetted" and allow_notgetted)
    return {
        ok = true,
        completed = completed,
        status = status,
        payload = {
            provider = "subgram",
            status = status,
            completed = completed
        }
    }
end

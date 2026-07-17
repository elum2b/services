function callback(event)
    local request = event.request or {}
    local body = request.body or {}
    local status = body.status or body.event or body.action or body.type
    local action = "complete"
    if status == "unsubscribe" or status == "unsubscribed" or status == "revoked" or status == "cancelled" or status == "canceled" then
        action = "revoked"
    end
    local external_id = body.external_id or body.offer_id or body.task_id
    local external_link = body.external_link or body.offer_link or body.link
    local platform_user_id = body.platform_user_id or body.tg_user_id or body.user_id
    return {
        ok = true,
        action = action,
        status = status or action,
        issue_id = body.issue_id,
        issue_ref = body.issue_ref or body.task_ref,
        external_click_id = body.external_click_id or body.click_id,
        external_id = external_id,
        platform_user_id = platform_user_id,
        lookup = {
            platform_user_id = platform_user_id,
            private_payload = {
                {
                    key = "link",
                    value = external_link
                }
            }
        },
        payload = {
            provider = "tgrass",
            status = status,
            event = body.event,
            offer_id = body.offer_id,
            offer_link = body.offer_link,
            task_id = body.task_id,
            tg_user_id = body.tg_user_id,
            is_fake = body.is_fake
        }
    }
end

function tgrass_base_url(event)
    event.config.settings = event.config.settings or {}
    return event.config.settings.base_url or "https://tgrass.space"
end

function list(event)
    event.variables = event.variables or {}
    event.config.settings = event.config.settings or {}
    local body = {
        tg_user_id = tonumber(event.identity.platform_user_id) or event.identity.platform_user_id,
        is_premium = event.identity.is_premium,
        lang = event.locale
    }
    if event.variables.tg_login ~= nil then
        body.tg_login = event.variables.tg_login
    elseif event.variables.username ~= nil then
        body.tg_login = event.variables.username
    end
    if event.variables.gender ~= nil then
        body.gender = event.variables.gender
    end
    if event.limit ~= nil and event.limit > 0 then
        body.offers_limit = event.limit
    end
    local path = "/offers"
    if event.config.settings.list_endpoint == "tasks" then
        path = "/tasks"
    end
    local response = http.request({
        method = "POST",
        url = tgrass_base_url(event) .. path,
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
    if data.status == "no_offers" then
        return {
            ok = true,
            tasks = {}
        }
    end
    local tasks = {}
    for _, offer in ipairs(data.offers or {}) do
        if offer.link ~= nil and offer.offer_id ~= nil and offer.subscribed ~= true then
            local external_type = offer.type or "offer"
            local button_text = "Перейти"
            if external_type == "channel" or external_type == "folder" then
                button_text = "Подписаться"
            end
            table.insert(tasks, {
                external_id = tostring(offer.offer_id),
                external_type = external_type,
                public_payload = {
                    offer_id = offer.offer_id,
                    channel_id = offer.channel_id,
                    name = offer.name or "",
                    link = offer.link,
                    button_text = button_text,
                    tgrass_status = data.status,
                    subscribed = offer.subscribed
                },
                private_payload = {
                    offer_id = offer.offer_id,
                    link = offer.link,
                    type = external_type
                }
            })
        end
    end
    return {
        ok = true,
        tasks = tasks
    }
end

function check(event)
    event.variables = event.variables or {}
    event.config.settings = event.config.settings or {}
    local private = event.issue.private_payload or {}
    local offer_id = private.offer_id or event.issue.external_id
    local response = http.request({
        method = "POST",
        url = tgrass_base_url(event) .. "/check",
        headers = {
            ["Auth"] = event.config.secret,
            ["Content-Type"] = "application/json"
        },
        body = json.encode({
            tg_user_id = tonumber(event.identity.platform_user_id) or event.identity.platform_user_id,
            offer_id = offer_id
        })
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
    local completed = data.status == "subscribed" and data.is_fake ~= true
    local status = data.status or "unknown"
    if data.is_fake == true then
        status = "fake"
    end
    return {
        ok = true,
        completed = completed,
        status = status,
        payload = {
            provider = "tgrass",
            status = data.status,
            is_fake = data.is_fake,
            completed = completed
        }
    }
end

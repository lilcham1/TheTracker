//! Native egui UI: Live / History / Leaderboard / Profile tabs.
//! Each render reads a cheap snapshot out of the shared tracker (a short
//! lock, then release) so the UI code never has to hold the mutex while
//! egui widgets borrow it — interactions instead re-lock briefly to apply
//! the change (toggle tracking, mark Roshan, set game type, save profile).

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use eframe::egui;

use crate::heroes;
use crate::model::{self, Checkpoint, CompareMetric, Death, KeyItemEntry, MatchState, MatchSummary, Profile};
use crate::state::{self, Tracker};
use crate::storage;

#[derive(PartialEq, Clone, Copy)]
enum Tab {
    Live,
    History,
    Leaderboard,
    Profile,
}

const LB_METRICS: [(&str, &str); 4] = [
    ("last_hits_25", "Most LH @25m"),
    ("fewest_deaths", "Fewest Deaths"),
    ("least_gold_lost", "Least Gold Lost"),
    ("most_kills", "Most Kills"),
];

pub struct App {
    tracker: Arc<Mutex<Tracker>>,
    server_error: Arc<Mutex<Option<String>>>,
    tab: Tab,
    profile_edit: Profile,
    profile_saved_flash: f32,
    lb_type: String,
    lb_metric: String,
}

impl App {
    pub fn new(tracker: Arc<Mutex<Tracker>>, server_error: Arc<Mutex<Option<String>>>) -> Self {
        App {
            tracker,
            server_error,
            tab: Tab::Live,
            profile_edit: storage::load_profile(),
            profile_saved_flash: 0.0,
            lb_type: "all".to_string(),
            lb_metric: "last_hits_25".to_string(),
        }
    }

    fn top_bar(&mut self, ui: &mut egui::Ui) {
        ui.add_space(6.0);
        ui.vertical_centered(|ui| {
            ui.heading("\u{2694} Dota Tracker");
            ui.label(egui::RichText::new("Match Intelligence").small().weak());
        });

        let has_profile_info = !self.profile_edit.username.is_empty()
            || self.profile_edit.rank.is_some()
            || self.profile_edit.role.is_some();
        if has_profile_info {
            ui.vertical_centered(|ui| {
                ui.horizontal(|ui| {
                    ui.add_space(ui.available_width() / 2.0 - 60.0);
                    if !self.profile_edit.username.is_empty() {
                        ui.label(self.profile_edit.username.clone());
                    }
                    if let Some(id) = self.profile_edit.rank.as_deref() {
                        if let Some(info) = model::RANKS.iter().find(|r| r.id == id) {
                            ui.colored_label(color_of(info.color), info.label);
                        }
                    }
                    if let Some(id) = self.profile_edit.role.as_deref() {
                        if let Some(info) = model::ROLES.iter().find(|r| r.id == id) {
                            ui.colored_label(color_of(info.color), info.label);
                        }
                    }
                });
            });
        }

        if let Some(err) = self.server_error.lock().unwrap().clone() {
            ui.colored_label(color_of((224, 104, 95)), format!("\u{26A0} {err}"));
        }

        ui.add_space(6.0);
        ui.horizontal(|ui| {
            if ui.selectable_label(self.tab == Tab::Live, "Live").clicked() {
                self.tab = Tab::Live;
            }
            if ui.selectable_label(self.tab == Tab::History, "History").clicked() {
                self.tab = Tab::History;
            }
            if ui.selectable_label(self.tab == Tab::Leaderboard, "Leaderboard").clicked() {
                self.tab = Tab::Leaderboard;
            }
            if ui.selectable_label(self.tab == Tab::Profile, "Profile").clicked() {
                self.tab = Tab::Profile;
            }
        });

        ui.horizontal(|ui| {
            let mut enabled = self.tracker.lock().unwrap().tracking_enabled;
            let resp = ui.toggle_value(&mut enabled, "Tracking");
            if resp.changed() {
                self.tracker.lock().unwrap().tracking_enabled = enabled;
            }
            ui.label(if enabled { "ON" } else { "OFF" });
        });
        ui.separator();
    }

    fn live_tab(&mut self, ui: &mut egui::Ui) {
        let snapshot: Option<MatchState> = self.tracker.lock().unwrap().current.clone();
        let Some(m) = snapshot else {
            ui.weak("Waiting for a match to start...");
            return;
        };

        if m.ended {
            if let Some(summary) = &m.summary {
                summary_card_ui(ui, summary);
            }
        }

        ui.horizontal(|ui| {
            if let Some(clean) = heroes::hero_clean_name(m.hero_name.as_deref()) {
                let url = format!("{}{}.png", heroes::HERO_CDN, clean);
                ui.add(egui::Image::new(url).max_width(48.0).max_height(48.0));
            }
            ui.vertical(|ui| {
                ui.label(egui::RichText::new(heroes::hero_display_name(m.hero_name.as_deref())).size(20.0).strong());
                ui.weak(state::fmt_clock(Some(m.last_clock_time)));
            });
        });

        let mut clicked_game_type: Option<&'static str> = None;
        ui.horizontal(|ui| {
            for gt in model::GAME_TYPES {
                if ui.selectable_label(m.game_type == gt, model::game_type_label(gt)).clicked() {
                    clicked_game_type = Some(gt);
                }
            }
        });
        if let Some(gt) = clicked_game_type {
            self.tracker.lock().unwrap().set_game_type(gt);
        }

        ui.add_space(8.0);
        ui.columns(2, |cols| {
            cols[0].group(|ui| {
                ui.weak("Last Hits / Denies");
                ui.label(egui::RichText::new(format!("{} / {}", m.last_hits, m.denies)).size(22.0).strong());
            });
            cols[1].group(|ui| {
                ui.weak("Deaths / Gold Lost");
                ui.colored_label(
                    color_of((224, 104, 95)),
                    egui::RichText::new(format!("{} / {}g", m.deaths.len(), m.total_gold_lost())).size(22.0).strong(),
                );
            });
        });

        ui.add_space(8.0);
        if roshan_card_ui(ui, &m.roshan, m.last_clock_time) {
            self.tracker.lock().unwrap().mark_roshan_death("manual");
        }

        ui.add_space(8.0);
        ui.label(egui::RichText::new("Last Hit Checkpoints").strong());
        checkpoints_row_ui(ui, &m.checkpoints);

        ui.add_space(8.0);
        ui.label(egui::RichText::new("Key Items").strong());
        key_item_grid_ui(ui, &m.key_item_log);

        ui.add_space(8.0);
        ui.label(egui::RichText::new("Deaths").strong());
        deaths_list_ui(ui, &m.deaths);
        ui.add_space(20.0);
    }

    fn history_tab(&mut self, ui: &mut egui::Ui) {
        let history = storage::load_history();
        if history.is_empty() {
            ui.weak("No finished matches yet.");
            return;
        }
        for m in history.iter().rev() {
            let title = format!(
                "{} \u{2014} {} [{}]",
                heroes::hero_display_name(m.hero_name.as_deref()),
                format_date(&m.date),
                model::game_type_label(&m.game_type)
            );
            egui::CollapsingHeader::new(title)
                .id_source(&m.matchid)
                .show(ui, |ui| {
                    ui.weak(format!(
                        "{} \u{b7} {} deaths \u{b7} {}g lost \u{b7} Rosh x{}",
                        m.duration, m.total_deaths, m.total_gold_lost, m.roshan_deaths
                    ));
                    comparison_block_ui(ui, m);
                    ui.add_space(6.0);
                    ui.label(egui::RichText::new("Key Items").strong());
                    key_item_grid_ui(ui, &m.key_items);
                    ui.add_space(6.0);
                    ui.label(egui::RichText::new("Deaths").strong());
                    deaths_list_ui(ui, &m.deaths);
                });
        }
        ui.add_space(20.0);
    }

    fn leaderboard_tab(&mut self, ui: &mut egui::Ui) {
        let history = storage::load_history();

        ui.horizontal_wrapped(|ui| {
            let all_selected = self.lb_type == "all";
            if ui.selectable_label(all_selected, "All Types").clicked() {
                self.lb_type = "all".to_string();
            }
            for t in model::GAME_TYPES {
                if ui.selectable_label(self.lb_type == t, model::game_type_label(t)).clicked() {
                    self.lb_type = t.to_string();
                }
            }
        });
        ui.horizontal_wrapped(|ui| {
            for (key, label) in LB_METRICS {
                if ui.selectable_label(self.lb_metric == key, label).clicked() {
                    self.lb_metric = key.to_string();
                }
            }
        });
        ui.add_space(10.0);

        let mut ranked: Vec<(&MatchSummary, f64)> = history
            .iter()
            .filter(|m| self.lb_type == "all" || m.game_type == self.lb_type)
            .filter_map(|m| lb_metric_value(m, &self.lb_metric).map(|v| (m, v)))
            .collect();
        let higher_better = lb_higher_is_better(&self.lb_metric);
        ranked.sort_by(|a, b| {
            if higher_better {
                b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal)
            } else {
                a.1.partial_cmp(&b.1).unwrap_or(std::cmp::Ordering::Equal)
            }
        });
        ranked.truncate(10);

        if ranked.is_empty() {
            ui.weak("No games with this stat yet.");
            return;
        }
        let medals = ["\u{1F947}", "\u{1F948}", "\u{1F949}"];
        for (i, (m, v)) in ranked.iter().enumerate() {
            ui.horizontal(|ui| {
                let prefix = medals.get(i).copied().unwrap_or("");
                ui.label(format!("{prefix} #{} {}", i + 1, heroes::hero_display_name(m.hero_name.as_deref())));
                ui.label(lb_metric_fmt(&self.lb_metric, *v));
            });
        }
        ui.add_space(20.0);
    }

    fn profile_tab(&mut self, ui: &mut egui::Ui) {
        ui.label("Username");
        ui.text_edit_singleline(&mut self.profile_edit.username);
        ui.add_space(10.0);

        ui.label("Rank");
        ui.horizontal_wrapped(|ui| {
            for r in model::RANKS.iter() {
                let selected = self.profile_edit.rank.as_deref() == Some(r.id);
                if ui.selectable_label(selected, r.label).clicked() {
                    self.profile_edit.rank = Some(r.id.to_string());
                }
            }
        });
        ui.add_space(10.0);

        ui.label("Main Role");
        ui.horizontal_wrapped(|ui| {
            for r in model::ROLES.iter() {
                let selected = self.profile_edit.role.as_deref() == Some(r.id);
                if ui.selectable_label(selected, r.label).clicked() {
                    self.profile_edit.role = Some(r.id.to_string());
                }
            }
        });
        ui.add_space(14.0);

        if ui.button("Save Profile").clicked() {
            storage::save_profile(&self.profile_edit);
            self.profile_saved_flash = 2.0;
        }
        if self.profile_saved_flash > 0.0 {
            ui.colored_label(color_of((111, 209, 138)), "Saved!");
        }
        ui.add_space(20.0);
    }
}

impl eframe::App for App {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        let dt = ctx.input(|i| i.stable_dt);
        if self.profile_saved_flash > 0.0 {
            self.profile_saved_flash -= dt;
        }
        ctx.request_repaint_after(std::time::Duration::from_millis(400));

        egui::TopBottomPanel::top("top_panel").show(ctx, |ui| {
            self.top_bar(ui);
        });
        egui::CentralPanel::default().show(ctx, |ui| {
            egui::ScrollArea::vertical().show(ui, |ui| match self.tab {
                Tab::Live => self.live_tab(ui),
                Tab::History => self.history_tab(ui),
                Tab::Leaderboard => self.leaderboard_tab(ui),
                Tab::Profile => self.profile_tab(ui),
            });
        });
    }
}

fn color_of((r, g, b): (u8, u8, u8)) -> egui::Color32 {
    egui::Color32::from_rgb(r, g, b)
}

fn format_date(iso: &str) -> String {
    chrono::DateTime::parse_from_rfc3339(iso)
        .map(|d| d.format("%b %d, %H:%M").to_string())
        .unwrap_or_else(|_| iso.to_string())
}

fn format_num(v: f64) -> String {
    if (v.fract()).abs() < 1e-9 {
        format!("{}", v as i64)
    } else {
        format!("{:.1}", v)
    }
}

fn badge_ui(ui: &mut egui::Ui, verdict: &str) {
    let (text, color) = match verdict {
        "better" => ("Better", (111, 209, 138)),
        "worse" => ("Worse", (224, 104, 95)),
        "similar" => ("Average", (154, 144, 136)),
        _ => ("New", (107, 98, 88)),
    };
    ui.colored_label(color_of(color), text);
}

fn comparison_row(ui: &mut egui::Ui, label: &str, comp: &CompareMetric) {
    ui.horizontal(|ui| {
        ui.label(label);
        let val_str = comp.value.map(format_num).unwrap_or_else(|| "\u{2014}".to_string());
        let avg_str = comp.avg.map(|a| format!(" (avg {})", format_num(a))).unwrap_or_default();
        ui.label(format!("{val_str}{avg_str}"));
        badge_ui(ui, &comp.verdict);
        if comp.is_best {
            ui.label("\u{1F3C6}");
        }
    });
}

fn comparison_block_ui(ui: &mut egui::Ui, s: &MatchSummary) {
    if let Some(cmp) = &s.comparison {
        ui.weak(format!(
            "vs your last {} {} games",
            s.games_compared_against.unwrap_or(0),
            model::game_type_label(&s.game_type)
        ));
        comparison_row(ui, "Deaths", &cmp.deaths);
        comparison_row(ui, "Gold Lost to Deaths", &cmp.gold_lost);
        for min in model::CHECKPOINT_MINUTES {
            if let Some(c) = cmp.checkpoints.get(&min) {
                if c.value.is_some() {
                    comparison_row(ui, &format!("{min} min Last Hits"), c);
                }
            }
        }
    }
}

fn summary_card_ui(ui: &mut egui::Ui, s: &MatchSummary) {
    ui.group(|ui| {
        ui.label(
            egui::RichText::new(format!("\u{1F3C1} Match Summary \u{2014} {}", model::game_type_label(&s.game_type)))
                .strong()
                .size(16.0),
        );
        ui.weak(format!("{} \u{2014} {}", heroes::hero_display_name(s.hero_name.as_deref()), s.duration));
        ui.add_space(6.0);
        comparison_block_ui(ui, s);
    });
    ui.add_space(10.0);
}

/// Returns true if the "Mark Roshan Death" button was clicked this frame.
fn roshan_card_ui(ui: &mut egui::Ui, roshan: &crate::model::RoshanState, clock_time_raw: f64) -> bool {
    let mut clicked = false;
    ui.group(|ui| {
        let (status, mut sub) = match roshan.last_death_clock {
            None => ("\u{1F409} Roshan \u{2014} Alive".to_string(), "No deaths recorded yet this game".to_string()),
            Some(last) => {
                let min_respawn = last + 480.0;
                let max_respawn = last + 660.0;
                let t = clock_time_raw;
                let countdown = |target: f64| -> String {
                    let rem = (target - t).max(0.0);
                    format!("{}:{:02}", (rem / 60.0).floor() as i64, (rem % 60.0).floor() as i64)
                };
                if t < min_respawn {
                    ("\u{1F409} Roshan \u{2014} Dead".to_string(), format!("Respawn window opens in {}", countdown(min_respawn)))
                } else if t < max_respawn {
                    ("\u{1F409} Roshan \u{2014} Maybe Alive".to_string(), format!("Guaranteed alive in {}", countdown(max_respawn)))
                } else {
                    ("\u{1F409} Roshan \u{2014} Alive".to_string(), "Respawn window has passed".to_string())
                }
            }
        };
        if roshan.last_death_clock.is_some() {
            sub.push_str(&format!(" \u{b7} Death #{} \u{2014} drops: {}", roshan.deaths, heroes::roshan_drops(roshan.deaths)));
        }
        ui.label(egui::RichText::new(status).strong());
        ui.weak(sub);
        if ui.button("Mark Roshan Death").clicked() {
            clicked = true;
        }
    });
    clicked
}

fn checkpoints_row_ui(ui: &mut egui::Ui, checkpoints: &BTreeMap<u32, Option<Checkpoint>>) {
    ui.horizontal(|ui| {
        for min in model::CHECKPOINT_MINUTES {
            ui.vertical(|ui| {
                ui.weak(format!("{min}m"));
                match checkpoints.get(&min).and_then(|c| *c) {
                    Some(c) => {
                        ui.label(format!("{}", c.last_hits));
                    }
                    None => {
                        ui.weak("\u{2014}");
                    }
                }
            });
        }
    });
}

fn key_item_grid_ui(ui: &mut egui::Ui, items: &[KeyItemEntry]) {
    if items.is_empty() {
        ui.weak("No key items yet");
        return;
    }
    ui.horizontal_wrapped(|ui| {
        for it in items {
            ui.vertical(|ui| {
                let url = format!("{}{}.png", heroes::ITEM_CDN, it.item);
                ui.add(egui::Image::new(url).max_width(64.0).max_height(48.0));
                ui.label(egui::RichText::new(it.clock.clone()).small());
            });
        }
    });
}

fn deaths_list_ui(ui: &mut egui::Ui, deaths: &[Death]) {
    if deaths.is_empty() {
        ui.weak("No deaths yet");
        return;
    }
    for d in deaths {
        ui.horizontal(|ui| {
            ui.label(d.clock.clone());
            let gold_str = d.gold_lost.map(|g| format!("-{g}g")).unwrap_or_else(|| "-?g".to_string());
            ui.colored_label(color_of((224, 104, 95)), gold_str);
        });
    }
}

fn lb_metric_value(m: &MatchSummary, key: &str) -> Option<f64> {
    match key {
        "last_hits_25" => m.checkpoints.get(&25).and_then(|c| c.map(|cc| cc.last_hits as f64)),
        "fewest_deaths" => Some(m.total_deaths as f64),
        "least_gold_lost" => Some(m.total_gold_lost as f64),
        "most_kills" => Some(m.kills as f64),
        _ => None,
    }
}

fn lb_higher_is_better(key: &str) -> bool {
    matches!(key, "last_hits_25" | "most_kills")
}

fn lb_metric_fmt(key: &str, v: f64) -> String {
    let iv = v as i64;
    match key {
        "last_hits_25" => format!("{iv} LH"),
        "fewest_deaths" => format!("{iv} deaths"),
        "least_gold_lost" => format!("{iv}g lost"),
        "most_kills" => format!("{iv} kills"),
        _ => format!("{iv}"),
    }
}
